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
	if config.RabbitMQ.URL != "http://127.0.0.1:15672" || config.RabbitMQ.PollInterval != time.Minute {
		t.Errorf("RabbitMQ settings = (%q, %s), want example URL and 1m interval", config.RabbitMQ.URL, config.RabbitMQ.PollInterval)
	}
	if len(config.Systemd.Units) != 0 {
		t.Errorf("systemd units = %v, want no units in the example configuration", config.Systemd.Units)
	}
	if config.Alerts.ReminderInterval != 24*time.Hour || len(config.Alerts.Recipients) != 1 {
		t.Errorf("alert settings = (%s, %v), want 24h and one recipient", config.Alerts.ReminderInterval, config.Alerts.Recipients)
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
