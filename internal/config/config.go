package config

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server     ServerConfig
	Database   DatabaseConfig
	Admin      AdminConfig
	Monitoring MonitoringConfig
	RabbitMQ   RabbitMQConfig
	Systemd    SystemdConfig
	SMTP       SMTPConfig
	Alerts     AlertConfig
}

type MonitoringConfig struct {
	PollInterval time.Duration
}

type ServerConfig struct {
	Host           string
	Port           int
	SecureCookies  bool
	TrustedProxies []string
}

func (server ServerConfig) Address() string {
	host := server.Host
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(server.Port))
}

type DatabaseConfig struct {
	Path string
}

type AdminConfig struct {
	Username          string
	Email             string
	Password          string
	IsChangedPassword bool
}

type RabbitMQConfig struct {
	URL              string
	Username         string
	Password         string
	VirtualHosts     []string
	ExpectedQueues   []string
	ExcludedQueues   []string
	HistoryRetention time.Duration
}

type SystemdConfig struct {
	Units           []string
	Targets         []SystemdTarget
	SSHIdentityFile string
	SSHKnownHosts   string
}

type SystemdTarget struct {
	Host          string
	Unit          string
	ExpectedState string
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	StartTLS bool
}

type AlertConfig struct {
	Recipients       []string
	ReminderInterval time.Duration
}

var (
	systemdUnitPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]*\.service$`)
	sshHostPattern     = regexp.MustCompile(`^(?:[A-Za-z0-9][A-Za-z0-9._-]*@)?[A-Za-z0-9][A-Za-z0-9._-]*$`)
	statePattern       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
)

func (config AdminConfig) RequiresPasswordChange() bool {
	return config.IsChangedPassword || strings.TrimSpace(config.Password) != ""
}

func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, fmt.Errorf("config path cannot be empty")
	}

	settings := viper.New()
	settings.SetConfigFile(path)
	settings.SetConfigType("ini")
	settings.SetDefault("server.host", "127.0.0.1")
	settings.SetDefault("server.port", 8080)
	settings.SetDefault("server.secure_cookies", false)
	settings.SetDefault("database.path", "./data/moitoring.db")
	settings.SetDefault("admin.username", "admin")
	settings.SetDefault("admin.email", "admin@example.local")
	settings.SetDefault("admin.is_changed_password", false)
	settings.SetDefault("rabbitmq.virtual_hosts", "/")
	settings.SetDefault("rabbitmq.history_retention", "24h")
	settings.SetDefault("smtp.port", 587)
	settings.SetDefault("smtp.starttls", true)
	settings.SetDefault("alerts.reminder_interval", "24h")

	if err := settings.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	systemdTargets, err := parseSystemdTargets(settings.GetString("systemd.targets"))
	if err != nil {
		return Config{}, err
	}
	pollInterval := time.Minute
	if settings.IsSet("rabbitmq.poll_interval") {
		pollInterval = settings.GetDuration("rabbitmq.poll_interval")
	}
	if settings.IsSet("monitoring.poll_interval") {
		pollInterval = settings.GetDuration("monitoring.poll_interval")
	}

	config := Config{
		Server: ServerConfig{
			Host:           strings.TrimSpace(settings.GetString("server.host")),
			Port:           settings.GetInt("server.port"),
			SecureCookies:  settings.GetBool("server.secure_cookies"),
			TrustedProxies: splitList(settings.GetString("server.trusted_proxies")),
		},
		Database: DatabaseConfig{
			Path: strings.TrimSpace(settings.GetString("database.path")),
		},
		Admin: AdminConfig{
			Username:          strings.TrimSpace(settings.GetString("admin.username")),
			Email:             strings.TrimSpace(settings.GetString("admin.email")),
			Password:          settings.GetString("admin.password"),
			IsChangedPassword: settings.GetBool("admin.is_changed_password"),
		},
		Monitoring: MonitoringConfig{
			PollInterval: pollInterval,
		},
		RabbitMQ: RabbitMQConfig{
			URL:              strings.TrimRight(strings.TrimSpace(settings.GetString("rabbitmq.url")), "/"),
			Username:         strings.TrimSpace(settings.GetString("rabbitmq.username")),
			Password:         settings.GetString("rabbitmq.password"),
			VirtualHosts:     splitList(settings.GetString("rabbitmq.virtual_hosts")),
			ExpectedQueues:   splitList(settings.GetString("rabbitmq.expected_queues")),
			ExcludedQueues:   splitList(settings.GetString("rabbitmq.excluded_queues")),
			HistoryRetention: settings.GetDuration("rabbitmq.history_retention"),
		},
		Systemd: SystemdConfig{
			Units:           splitList(settings.GetString("systemd.units")),
			Targets:         systemdTargets,
			SSHIdentityFile: strings.TrimSpace(settings.GetString("systemd.ssh_identity_file")),
			SSHKnownHosts:   strings.TrimSpace(settings.GetString("systemd.ssh_known_hosts")),
		},
		SMTP: SMTPConfig{
			Host:     strings.TrimSpace(settings.GetString("smtp.host")),
			Port:     settings.GetInt("smtp.port"),
			Username: strings.TrimSpace(settings.GetString("smtp.username")),
			Password: settings.GetString("smtp.password"),
			From:     strings.TrimSpace(settings.GetString("smtp.from")),
			StartTLS: settings.GetBool("smtp.starttls"),
		},
		Alerts: AlertConfig{
			Recipients:       splitList(settings.GetString("alerts.recipients")),
			ReminderInterval: settings.GetDuration("alerts.reminder_interval"),
		},
	}
	if config.Server.Host == "" {
		config.Server.Host = "127.0.0.1"
	}
	if config.Server.Port < 1 || config.Server.Port > 65535 {
		return Config{}, fmt.Errorf("server.port must be between 1 and 65535: %d", config.Server.Port)
	}
	if config.Monitoring.PollInterval <= 0 {
		return Config{}, fmt.Errorf("monitoring.poll_interval must be greater than zero")
	}
	if config.Database.Path == "" {
		return Config{}, fmt.Errorf("database.path cannot be empty")
	}

	return config, nil
}

func (config Config) ValidateWorker() error {
	endpoint, err := url.Parse(config.RabbitMQ.URL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return fmt.Errorf("rabbitmq.url must be an absolute HTTP or HTTPS URL")
	}
	if config.RabbitMQ.Username == "" || config.RabbitMQ.Password == "" {
		return fmt.Errorf("rabbitmq.username and rabbitmq.password are required")
	}
	if len(config.RabbitMQ.VirtualHosts) == 0 {
		return fmt.Errorf("rabbitmq.virtual_hosts must contain at least one virtual host")
	}
	if config.RabbitMQ.HistoryRetention <= 0 {
		return fmt.Errorf("rabbitmq.history_retention must be greater than zero")
	}
	if config.SMTP.Host == "" || config.SMTP.Port < 1 || config.SMTP.Port > 65535 {
		return fmt.Errorf("smtp.host and a valid smtp.port are required")
	}
	if !config.SMTP.StartTLS {
		return fmt.Errorf("smtp.starttls must be enabled for alert delivery")
	}
	if config.SMTP.Username != "" && config.SMTP.Password == "" {
		return fmt.Errorf("smtp.password is required when smtp.username is set")
	}
	if _, err := parseEmail(config.SMTP.From); err != nil {
		return fmt.Errorf("smtp.from must be a valid email address")
	}
	if len(config.Alerts.Recipients) == 0 {
		return fmt.Errorf("alerts.recipients must contain at least one email address")
	}
	for _, recipient := range config.Alerts.Recipients {
		if _, err := parseEmail(recipient); err != nil {
			return fmt.Errorf("alerts.recipients contains an invalid email address")
		}
	}
	if config.Alerts.ReminderInterval <= 0 {
		return fmt.Errorf("alerts.reminder_interval must be greater than zero")
	}
	seenTargets := make(map[string]struct{}, len(config.Systemd.Units)+len(config.Systemd.Targets))
	remoteTargets := false
	validateTarget := func(target SystemdTarget) error {
		if target.Host == "" {
			target.Host = "localhost"
		}
		if target.Host != "localhost" && !sshHostPattern.MatchString(target.Host) {
			return fmt.Errorf("systemd.targets contains an invalid host")
		}
		if !systemdUnitPattern.MatchString(target.Unit) {
			return fmt.Errorf("systemd.targets must contain valid .service unit names")
		}
		if !statePattern.MatchString(target.ExpectedState) {
			return fmt.Errorf("systemd.targets expected state must be a simple state name")
		}
		key := target.Host + "\x00" + target.Unit
		if _, duplicate := seenTargets[key]; duplicate {
			return fmt.Errorf("systemd.targets contains a duplicate host and unit")
		}
		seenTargets[key] = struct{}{}
		remoteTargets = remoteTargets || target.Host != "localhost"
		return nil
	}
	for _, unit := range config.Systemd.Units {
		if err := validateTarget(SystemdTarget{Host: "localhost", Unit: unit, ExpectedState: "active"}); err != nil {
			return err
		}
	}
	for _, target := range config.Systemd.Targets {
		if err := validateTarget(target); err != nil {
			return err
		}
	}
	if remoteTargets && (!filepath.IsAbs(config.Systemd.SSHIdentityFile) || !filepath.IsAbs(config.Systemd.SSHKnownHosts)) {
		return fmt.Errorf("systemd.ssh_identity_file and systemd.ssh_known_hosts must be absolute paths for remote targets")
	}
	return nil
}

func parseSystemdTargets(value string) ([]SystemdTarget, error) {
	entries := splitList(value)
	targets := make([]SystemdTarget, 0, len(entries))
	for _, entry := range entries {
		fields := strings.Split(entry, "|")
		if len(fields) != 3 {
			return nil, fmt.Errorf("systemd.targets entries must use host|unit.service|expected_state")
		}
		target := SystemdTarget{
			Host:          strings.TrimSpace(fields[0]),
			Unit:          strings.TrimSpace(fields[1]),
			ExpectedState: strings.TrimSpace(fields[2]),
		}
		if target.ExpectedState == "" {
			target.ExpectedState = "active"
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func parseEmail(value string) (string, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil || address.Address != strings.TrimSpace(value) {
		return "", fmt.Errorf("invalid email")
	}
	return address.Address, nil
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
}

func CompleteAdminPasswordChange(path string) error {
	settings := viper.New()
	settings.SetConfigFile(path)
	settings.SetConfigType("ini")
	settings.SetConfigPermissions(0o600)
	if err := settings.ReadInConfig(); err != nil {
		return fmt.Errorf("read config while completing password change: %w", err)
	}
	settings.SetConfigType("ini")
	settings.Set("admin.password", "")
	settings.Set("admin.is_changed_password", false)
	if err := settings.WriteConfig(); err != nil {
		return fmt.Errorf("clear admin bootstrap settings: %w", err)
	}
	return nil
}
