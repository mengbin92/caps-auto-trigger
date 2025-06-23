package main

type TimeRange struct {
	Start string `yaml:"start"`
	End   string `yaml:"end"`
}

type LogConfig struct {
	Level string `yaml:"level"`
	Name  string `yaml:"name"`
}

type Config struct {
	Log        LogConfig    `yaml:"log"`
	Ticker     int          `yaml:"ticker"` // 触发间隔（秒）
	TimeRanges []*TimeRange `yaml:"time_ranges"`
	Daemon     bool         `yaml:"daemon"`
}
