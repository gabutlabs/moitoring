package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadINIExample(t *testing.T) {
	config, err := Load("../../config.ini.example")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Server.Port != 8080 {
		t.Errorf("server port = %d, want 8080", config.Server.Port)
	}
	if !config.Server.WebUIEnabled {
		t.Error("server web UI enabled = false, want true in the example")
	}
	if config.Server.Host != "127.0.0.1" {
		t.Errorf("server host = %q, want loopback", config.Server.Host)
	}
	if config.Server.SecureCookies {
		t.Error("server secure cookies = true, want false for the local HTTP example")
	}
	if len(config.Server.TrustedProxies) != 0 {
		t.Errorf("server trusted proxies = %v, want none in the example", config.Server.TrustedProxies)
	}
	if config.Database.Path != "./data/moitoring.db" {
		t.Errorf("database path = %q, want %q", config.Database.Path, "./data/moitoring.db")
	}
	if config.Admin.Username != "admin" {
		t.Errorf("admin username = %q, want %q", config.Admin.Username, "admin")
	}
	if config.Admin.Email != "admin@example.local" {
		t.Errorf("admin email = %q, want %q", config.Admin.Email, "admin@example.local")
	}
	if config.Admin.Password != "replace-this-before-use" {
		t.Errorf("admin password = %q, want configured example value", config.Admin.Password)
	}
	if !config.Admin.IsChangedPassword {
		t.Error("admin is_changed_password = false, want true in the example")
	}
	if config.RabbitMQ.URL != "http://127.0.0.1:15672" {
		t.Errorf("RabbitMQ URL = %q, want example URL", config.RabbitMQ.URL)
	}
	if config.Monitoring.PollInterval != time.Minute {
		t.Errorf("monitoring poll interval = %s, want 1m", config.Monitoring.PollInterval)
	}
	if len(config.Systemd.Units) != 0 {
		t.Errorf("systemd units = %v, want no units in the example configuration", config.Systemd.Units)
	}
	if config.Alerts.ReminderInterval != 24*time.Hour || len(config.Alerts.Recipients) != 1 {
		t.Errorf("alert settings = (%s, %v), want 24h and one recipient", config.Alerts.ReminderInterval, config.Alerts.Recipients)
	}
}

func TestLoadWebUISettingDefaultsEnabledAndCanBeDisabled(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		want    bool
	}{
		{name: "default", want: true},
		{name: "enabled", content: "[server]\nweb_ui_enabled = true\n", want: true},
		{name: "disabled", content: "[server]\nweb_ui_enabled = false\n", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.ini.example")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatalf("write example config: %v", err)
			}
			settings, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if settings.Server.WebUIEnabled != test.want {
				t.Errorf("web UI enabled = %t, want %t", settings.Server.WebUIEnabled, test.want)
			}
		})
	}
}

func TestLoadMonitoringPollInterval(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    time.Duration
		wantErr bool
	}{
		{name: "default", want: time.Minute},
		{name: "legacy RabbitMQ setting", content: "[rabbitmq]\npoll_interval = 35s\n", want: 35 * time.Second},
		{name: "monitoring setting takes precedence", content: "[rabbitmq]\npoll_interval = 35s\n[monitoring]\npoll_interval = 45s\n", want: 45 * time.Second},
		{name: "reject nonpositive interval", content: "[monitoring]\npoll_interval = 0s\n", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.ini")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			config, err := Load(path)
			if test.wantErr {
				if err == nil {
					t.Fatal("Load() error = nil, want invalid monitoring interval error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if config.Monitoring.PollInterval != test.want {
				t.Errorf("monitoring poll interval = %s, want %s", config.Monitoring.PollInterval, test.want)
			}
		})
	}
}

func TestLoadExpectedQueues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.ini.example")
	if err := os.WriteFile(path, []byte("[rabbitmq]\nexpected_queues = jobs, invoices\n"), 0o600); err != nil {
		t.Fatalf("write example config: %v", err)
	}

	settings, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(settings.RabbitMQ.ExpectedQueues) != 2 || settings.RabbitMQ.ExpectedQueues[0] != "jobs" || settings.RabbitMQ.ExpectedQueues[1] != "invoices" {
		t.Fatalf("expected queues = %v, want jobs and invoices", settings.RabbitMQ.ExpectedQueues)
	}
}

func TestServerConfigAddress(t *testing.T) {
	for _, test := range []struct {
		host string
		want string
	}{
		{host: "127.0.0.1", want: "127.0.0.1:8080"},
		{host: "0.0.0.0", want: "0.0.0.0:8080"},
		{host: "::", want: "[::]:8080"},
		{host: "", want: "127.0.0.1:8080"},
	} {
		settings := ServerConfig{Host: test.host, Port: 8080}
		if got := settings.Address(); got != test.want {
			t.Errorf("Address() with host %q = %q, want %q", test.host, got, test.want)
		}
	}
}

func TestSplitListParsesConfiguredSystemdUnits(t *testing.T) {
	units := splitList("jobs-worker.service, email-worker.service")
	if len(units) != 2 || units[0] != "jobs-worker.service" || units[1] != "email-worker.service" {
		t.Fatalf("systemd units = %v, want the two configured service names", units)
	}
}

func TestParseSystemdTargetsDefaultsExpectedState(t *testing.T) {
	targets, err := parseSystemdTargets("worker-a|jobs.service|, ops@worker-b|billing.service|inactive")
	if err != nil {
		t.Fatalf("parseSystemdTargets() error = %v", err)
	}
	if len(targets) != 2 || targets[0] != (SystemdTarget{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active"}) || targets[1].ExpectedState != "inactive" {
		t.Fatalf("systemd targets = %+v, want default active and configured inactive", targets)
	}
}

func TestParseSystemdTargetsRejectsMalformedEntry(t *testing.T) {
	if _, err := parseSystemdTargets("worker-a|jobs.service"); err == nil {
		t.Fatal("parseSystemdTargets() error = nil, want malformed-entry error")
	}
}

func TestValidateWorkerChecksSystemdTargetsAndRemoteSSHPaths(t *testing.T) {
	base, err := Load("../../config.ini.example")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	tests := []struct {
		name     string
		settings SystemdConfig
		wantErr  bool
	}{
		{name: "local unit", settings: SystemdConfig{Units: []string{"jobs.service"}}},
		{name: "invalid local unit", settings: SystemdConfig{Units: []string{"--help"}}, wantErr: true},
		{name: "remote needs identity and known hosts", settings: SystemdConfig{Targets: []SystemdTarget{{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active"}}}, wantErr: true},
		{name: "remote pinned paths", settings: SystemdConfig{Targets: []SystemdTarget{{Host: "ops@worker-a", Unit: "jobs.service", ExpectedState: "active"}}, SSHIdentityFile: "/keys/id_ed25519", SSHKnownHosts: "/keys/known_hosts"}},
		{name: "remote shell injection rejected", settings: SystemdConfig{Targets: []SystemdTarget{{Host: "worker-a;id", Unit: "jobs.service", ExpectedState: "active"}}, SSHIdentityFile: "/keys/id_ed25519", SSHKnownHosts: "/keys/known_hosts"}, wantErr: true},
		{name: "expected state injection rejected", settings: SystemdConfig{Targets: []SystemdTarget{{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active;id"}}, SSHIdentityFile: "/keys/id_ed25519", SSHKnownHosts: "/keys/known_hosts"}, wantErr: true},
		{name: "duplicate targets rejected", settings: SystemdConfig{Units: []string{"jobs.service"}, Targets: []SystemdTarget{{Host: "localhost", Unit: "jobs.service", ExpectedState: "active"}}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := base
			settings.Systemd = test.settings
			err := settings.ValidateWorker()
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateWorker() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestValidateWorkerRequiresRabbitMQSMTPAndAlertSettings(t *testing.T) {
	settings, err := Load("../../config.ini.example")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := settings.ValidateWorker(); err != nil {
		t.Fatalf("ValidateWorker() with example settings error = %v", err)
	}

	settings.Alerts.Recipients = []string{"not-an-email"}
	if err := settings.ValidateWorker(); err == nil || strings.Contains(err.Error(), "not-an-email") {
		t.Fatalf("ValidateWorker() error = %v, want safe invalid-recipient error", err)
	}
}

func TestCompleteAdminPasswordChangeClearsBootstrapValues(t *testing.T) {
	contents, err := os.ReadFile("../../config.ini.example")
	if err != nil {
		t.Fatalf("read config example: %v", err)
	}
	path := filepath.Join(t.TempDir(), "settings.ini")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write temporary config: %v", err)
	}

	if err := CompleteAdminPasswordChange(path); err != nil {
		t.Fatalf("CompleteAdminPasswordChange() error = %v", err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after update error = %v", err)
	}
	if config.Admin.Password != "" {
		t.Errorf("admin password after change = %q, want empty", config.Admin.Password)
	}
	if config.Admin.IsChangedPassword {
		t.Error("admin is_changed_password after update = true, want false")
	}
}

func TestAdminRequiresPasswordChangeWhileBootstrapPasswordIsConfigured(t *testing.T) {
	for _, test := range []struct {
		name     string
		admin    AdminConfig
		expected bool
	}{
		{name: "explicit reset flag", admin: AdminConfig{IsChangedPassword: true}, expected: true},
		{name: "configured bootstrap password", admin: AdminConfig{Password: "bootstrap"}, expected: true},
		{name: "completed setup", admin: AdminConfig{}, expected: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.admin.RequiresPasswordChange(); got != test.expected {
				t.Errorf("RequiresPasswordChange() = %t, want %t", got, test.expected)
			}
		})
	}
}
