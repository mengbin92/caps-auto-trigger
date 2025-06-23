package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/micmonay/keybd_event"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"
)

var (
	logger     *zap.SugaredLogger
	config     Config
	configLock sync.RWMutex
	configPath = "config.yaml"
)

func main() {

	loadConfig()

	if config.Daemon {
		daemonize()
	}

	initLogger()
	logger.Info("CapsAutoTrigger 启动中...")

	go watchConfig(func() {
		logger.Info("配置已重新加载")
	})

	kb, err := keybd_event.NewKeyBonding()
	if err != nil {
		logger.Fatalf("键盘模拟器初始化失败: %v", err)
	}
	kb.SetKeys(keybd_event.VK_CAPSLOCK)

	ticker := time.NewTicker(time.Duration(config.Ticker) * time.Second)
	defer ticker.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			if inActiveTime() {
				logger.Infof("模拟双击 Caps Lock")
				if err := simulateDoubleCapsLock(kb); err != nil {
					logger.Errorf("按键失败: %v", err)
				}
			}
		case sig := <-sigChan:
			logger.Infof("收到退出信号: %v，程序退出", sig)
			return
		}
	}
}

func daemonize() {
	if os.Getenv("DAEMONIZED") == "1" {
		// 已经是子进程，无需再次守护化
		return
	}

	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = append(os.Environ(), "DAEMONIZED=1")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	err := cmd.Start()
	if err != nil {
		fmt.Println("后台启动失败:", err)
		os.Exit(1)
	}

	fmt.Println("程序已在后台运行（PID:", cmd.Process.Pid, "）")
	os.Exit(0)
}

// 模拟双击 Caps Lock
func simulateDoubleCapsLock(kb keybd_event.KeyBonding) error {
	if err := kb.Press(); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)
	if err := kb.Release(); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)
	if err := kb.Press(); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	return kb.Release()
}

// 加载配置
func loadConfig() {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		logger.Fatalf("读取配置失败: %v", err)
	}

	var newConfig Config
	if err := yaml.Unmarshal(data, &newConfig); err != nil {
		logger.Fatalf("解析配置失败: %v", err)
	}

	configLock.Lock()
	config = newConfig
	configLock.Unlock()
}

// 监听配置文件变化
func watchConfig(onReload func()) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Fatalf("初始化配置文件监听失败: %v", err)
	}
	defer watcher.Close()

	configDir := filepath.Dir(configPath)
	_ = watcher.Add(configDir)

	for {
		select {
		case event := <-watcher.Events:
			if filepath.Base(event.Name) == filepath.Base(configPath) &&
				(event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create) {
				logger.Info("检测到配置文件变更，重新加载配置")
				loadConfig()
				initLogger()
				onReload()
			}
		case err := <-watcher.Errors:
			logger.Errorf("配置监听错误: %v", err)
		}
	}
}

// 判断是否在配置的时间段中
func inActiveTime() bool {
	configLock.RLock()
	defer configLock.RUnlock()

	now := time.Now()
	for _, tr := range config.TimeRanges {
		start, _ := time.Parse("15:04", tr.Start)
		end, _ := time.Parse("15:04", tr.End)

		startTime := time.Date(now.Year(), now.Month(), now.Day(), start.Hour(), start.Minute(), 0, 0, now.Location())
		endTime := time.Date(now.Year(), now.Month(), now.Day(), end.Hour(), end.Minute(), 0, 0, now.Location())

		if now.After(startTime) && now.Before(endTime) {
			return true
		}
	}
	return false
}

// 初始化 zap 日志记录器
func initLogger() {
	configLock.RLock()
	logCfg := config.Log
	configLock.RUnlock()

	if logCfg.Name == "" {
		logCfg.Name = "keysimulator.log"
	}
	if logCfg.Level == "" {
		logCfg.Level = "info"
	}

	var lvl zapcore.Level
	err := lvl.UnmarshalText([]byte(logCfg.Level))
	if err != nil {
		lvl = zapcore.InfoLevel
	}

	cfg := zap.NewProductionEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(cfg),
		zapcore.AddSync(&lumberjackLogger{Filename: logCfg.Name}),
		lvl,
	)

	logger = zap.New(core).Sugar()
}

// 简易日志写入器
type lumberjackLogger struct {
	Filename string
}

func (l *lumberjackLogger) Write(p []byte) (n int, err error) {
	f, err := os.OpenFile(l.Filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Write(p)
}
