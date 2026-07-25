package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration struct {
	time.Duration
}

func NewDuration(d time.Duration) Duration {
	return Duration{Duration: d}
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Value == "" {
		d.Duration = 0
		return nil
	}
	var raw any
	if err := value.Decode(&raw); err != nil {
		return err
	}
	switch v := raw.(type) {
	case int:
		d.Duration = time.Duration(v)
	case int64:
		d.Duration = time.Duration(v)
	case float64:
		d.Duration = time.Duration(v)
	case string:
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("parse duration %q: %w", v, err)
		}
		d.Duration = parsed
	default:
		return fmt.Errorf("unsupported duration value %q", value.Value)
	}
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

type Config struct {
	Server     ServerConfig     `yaml:"server" json:"server"`
	Collect    CollectConfig    `yaml:"collect" json:"collect"`
	Logs       LogsConfig       `yaml:"logs" json:"logs"`
	Thresholds ThresholdConfig  `yaml:"thresholds" json:"thresholds"`
	Notify     NotifyRootConfig `yaml:"notify" json:"notify"`
}

type ServerConfig struct {
	Listen       string   `yaml:"listen" json:"listen"`
	ReadTimeout  Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout Duration `yaml:"write_timeout" json:"write_timeout"`
}

type CollectConfig struct {
	Interval          Duration `yaml:"interval" json:"interval"`
	NotifyCooldown    Duration `yaml:"notify_cooldown" json:"notify_cooldown"`
	DiskMounts        []string `yaml:"disk_mounts" json:"disk_mounts"`
	IgnoreFilesystems []string `yaml:"ignore_filesystems" json:"ignore_filesystems"`
}

type LogsConfig struct {
	MaxLines       int             `yaml:"max_lines" json:"max_lines"`
	IncludeDmesg   bool            `yaml:"include_dmesg" json:"include_dmesg"`
	IncludeJournal bool            `yaml:"include_journal" json:"include_journal"`
	Files          []LogFileConfig `yaml:"files" json:"files"`
	NotifyMinLevel string          `yaml:"notify_min_level" json:"notify_min_level"`
	ErrorPatterns  []string        `yaml:"error_patterns" json:"error_patterns"`
}

type LogFileConfig struct {
	Name    string `yaml:"name" json:"name"`
	Path    string `yaml:"path" json:"path"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

type ThresholdConfig struct {
	CPUPercent              float64 `yaml:"cpu_percent" json:"cpu_percent"`
	Load1PerCPU             float64 `yaml:"load1_per_cpu" json:"load1_per_cpu"`
	MemoryPercent           float64 `yaml:"memory_percent" json:"memory_percent"`
	SwapPercent             float64 `yaml:"swap_percent" json:"swap_percent"`
	DiskPercent             float64 `yaml:"disk_percent" json:"disk_percent"`
	DiskInodePercent        float64 `yaml:"disk_inode_percent" json:"disk_inode_percent"`
	TemperatureC            float64 `yaml:"temperature_c" json:"temperature_c"`
	ProcessCount            int     `yaml:"process_count" json:"process_count"`
	NetworkRxMbps           float64 `yaml:"network_rx_mbps" json:"network_rx_mbps"`
	NetworkTxMbps           float64 `yaml:"network_tx_mbps" json:"network_tx_mbps"`
	LogMatchCount           int     `yaml:"log_match_count" json:"log_match_count"`
	ConsecutiveTriggerCount int     `yaml:"consecutive_trigger_count" json:"consecutive_trigger_count"`
}

type NotifyRootConfig struct {
	Enabled  bool                 `yaml:"enabled" json:"enabled"`
	Channels NotifyChannelsConfig `yaml:"channels" json:"channels"`
}

type NotifyChannelsConfig struct {
	Stdout    StdoutNotifyConfig    `yaml:"stdout" json:"stdout"`
	FNOSEmail FNOSEmailNotifyConfig `yaml:"fnos_email" json:"fnos_email"`
	SMTP      SMTPNotifyConfig      `yaml:"smtp" json:"smtp"`
	Webhook   WebhookNotifyConfig   `yaml:"webhook" json:"webhook"`
}

type StdoutNotifyConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type FNOSEmailNotifyConfig struct {
	Enabled     bool              `yaml:"enabled" json:"enabled"`
	Mode        string            `yaml:"mode" json:"mode"`
	Timeout     Duration          `yaml:"timeout" json:"timeout"`
	Command     string            `yaml:"command" json:"command"`
	Args        []string          `yaml:"args" json:"args"`
	BodyStdin   bool              `yaml:"body_stdin" json:"body_stdin"`
	HTTPURL     string            `yaml:"http_url" json:"http_url"`
	HTTPMethod  string            `yaml:"http_method" json:"http_method"`
	HTTPHeaders map[string]string `yaml:"http_headers" json:"http_headers"`
}

type SMTPNotifyConfig struct {
	Enabled            bool     `yaml:"enabled" json:"enabled"`
	Host               string   `yaml:"host" json:"host"`
	Port               int      `yaml:"port" json:"port"`
	Username           string   `yaml:"username" json:"username"`
	Password           string   `yaml:"password" json:"-"`
	PasswordEnv        string   `yaml:"password_env" json:"password_env"`
	From               string   `yaml:"from" json:"from"`
	FromName           string   `yaml:"from_name" json:"from_name"`
	To                 []string `yaml:"to" json:"to"`
	StartTLS           bool     `yaml:"start_tls" json:"start_tls"`
	TLS                bool     `yaml:"tls" json:"tls"`
	InsecureSkipVerify bool     `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
}

type WebhookNotifyConfig struct {
	Enabled bool              `yaml:"enabled" json:"enabled"`
	URL     string            `yaml:"url" json:"url"`
	Method  string            `yaml:"method" json:"method"`
	Timeout Duration          `yaml:"timeout" json:"timeout"`
	Headers map[string]string `yaml:"headers" json:"headers"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			Listen:       ":39187",
			ReadTimeout:  NewDuration(5 * time.Second),
			WriteTimeout: NewDuration(10 * time.Second),
		},
		Collect: CollectConfig{
			Interval:       NewDuration(30 * time.Second),
			NotifyCooldown: NewDuration(10 * time.Minute),
			IgnoreFilesystems: []string{
				"tmpfs", "devtmpfs", "overlay", "squashfs", "proc", "sysfs",
				"cgroup", "cgroup2", "fusectl", "tracefs", "debugfs",
			},
		},
		Logs: LogsConfig{
			MaxLines:       300,
			IncludeDmesg:   true,
			IncludeJournal: true,
			Files: []LogFileConfig{
				{Name: "syslog", Path: "/var/log/syslog", Enabled: true},
				{Name: "messages", Path: "/var/log/messages", Enabled: true},
				{Name: "kernel", Path: "/var/log/kern.log", Enabled: true},
				{Name: "auth", Path: "/var/log/auth.log", Enabled: true},
			},
			NotifyMinLevel: "warning",
			ErrorPatterns: []string{
				`(?i)\berror\b`, `(?i)\bfailed\b`, `(?i)\bcritical\b`,
				`(?i)\bpanic\b`, `(?i)\bout of memory\b`, `(?i)\bio error\b`,
				`(?i)\bsmart.*fail`, `(?i)\bmdadm.*degrad`,
			},
		},
		Thresholds: ThresholdConfig{
			CPUPercent:              90,
			Load1PerCPU:             2,
			MemoryPercent:           90,
			SwapPercent:             98,
			DiskPercent:             85,
			DiskInodePercent:        85,
			TemperatureC:            75,
			ProcessCount:            1200,
			LogMatchCount:           1,
			ConsecutiveTriggerCount: 1,
		},
		Notify: NotifyRootConfig{
			Enabled: true,
			Channels: NotifyChannelsConfig{
				Stdout: StdoutNotifyConfig{Enabled: true},
				FNOSEmail: FNOSEmailNotifyConfig{
					Mode:       "command",
					Timeout:    NewDuration(15 * time.Second),
					HTTPMethod: "POST",
				},
				SMTP: SMTPNotifyConfig{
					Port:     587,
					StartTLS: true,
				},
				Webhook: WebhookNotifyConfig{
					Method:  "POST",
					Timeout: NewDuration(10 * time.Second),
				},
			},
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, cfg.Validate()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.Server.Listen == "" {
		return errors.New("server.listen is required")
	}
	if c.Collect.Interval.Duration <= 0 {
		return errors.New("collect.interval must be greater than zero")
	}
	if c.Collect.NotifyCooldown.Duration < 0 {
		return errors.New("collect.notify_cooldown cannot be negative")
	}
	if c.Thresholds.ConsecutiveTriggerCount <= 0 {
		return errors.New("thresholds.consecutive_trigger_count must be greater than zero")
	}
	if c.Logs.MaxLines <= 0 {
		return errors.New("logs.max_lines must be greater than zero")
	}
	switch c.Logs.NotifyMinLevel {
	case "", "info", "warning", "error", "critical", "none":
	default:
		return fmt.Errorf("unsupported logs.notify_min_level %q", c.Logs.NotifyMinLevel)
	}
	if c.Notify.Channels.FNOSEmail.Enabled {
		if c.Notify.Channels.FNOSEmail.Mode == "" {
			return errors.New("notify.channels.fnos_email.mode is required")
		}
		switch c.Notify.Channels.FNOSEmail.Mode {
		case "command":
			if c.Notify.Channels.FNOSEmail.Command == "" {
				return errors.New("notify.channels.fnos_email.command is required in command mode")
			}
		case "http":
			if c.Notify.Channels.FNOSEmail.HTTPURL == "" {
				return errors.New("notify.channels.fnos_email.http_url is required in http mode")
			}
		default:
			return fmt.Errorf("unsupported fnos_email mode %q", c.Notify.Channels.FNOSEmail.Mode)
		}
	}
	if c.Notify.Channels.SMTP.Enabled {
		if c.Notify.Channels.SMTP.Host == "" || c.Notify.Channels.SMTP.From == "" || len(c.Notify.Channels.SMTP.To) == 0 {
			return errors.New("notify.channels.smtp host, from and to are required")
		}
		if c.Notify.Channels.SMTP.Port == 0 {
			return errors.New("notify.channels.smtp.port is required")
		}
	}
	if c.Notify.Channels.Webhook.Enabled {
		if c.Notify.Channels.Webhook.URL == "" {
			return errors.New("notify.channels.webhook.url is required")
		}
	}
	return nil
}
