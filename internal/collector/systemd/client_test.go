package systemd

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"moitoring/internal/models"
)

type fakeRunner struct {
	output []byte
	err    error
	args   []string
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "/usr/bin/systemctl" && name != "/usr/bin/ssh" {
		return nil, errors.New("unexpected command")
	}
	r.args = append([]string(nil), args...)
	return r.output, r.err
}

func TestCollectParsesSystemdFailureDetails(t *testing.T) {
	runner := &fakeRunner{output: []byte(strings.Join([]string{
		"LoadState=loaded",
		"ActiveState=failed",
		"SubState=failed",
		"Result=exit-code",
		"ExecMainCode=1",
		"ExecMainStatus=1",
		"StatusText=worker exited",
	}, "\n"))}
	client := Client{systemctlPath: "/usr/bin/systemctl", runner: runner}

	workers, err := client.Collect(context.Background(), []models.WorkerTarget{{Unit: "jobs.service"}})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("Collect() returned %d workers, want 1", len(workers))
	}
	worker := workers[0]
	if worker.Host != "localhost" || worker.Unit != "jobs.service" || worker.ExpectedState != "active" {
		t.Fatalf("worker identity/expectation = %+v", worker)
	}
	if worker.LoadState != "loaded" || worker.ActiveState != "failed" || worker.SubState != "failed" || worker.Result != "exit-code" || worker.ExecMainStatus != "1" || worker.StatusText != "worker exited" {
		t.Fatalf("worker failure details = %+v", worker)
	}
	if len(runner.args) == 0 || runner.args[len(runner.args)-1] != "jobs.service" {
		t.Fatalf("systemctl arguments = %v, want target unit last", runner.args)
	}
}

func TestCollectRejectsOutputWithoutActiveState(t *testing.T) {
	client := Client{systemctlPath: "/usr/bin/systemctl", runner: &fakeRunner{output: []byte("SubState=dead\n")}}
	workers, err := client.Collect(context.Background(), []models.WorkerTarget{{Unit: "jobs.service"}})
	if err != nil || len(workers) != 1 || workers[0].CollectionError == "" {
		t.Fatalf("Collect() = (%+v, %v), want one per-target collection error", workers, err)
	}
}

func TestCollectPreservesNotFoundUnitState(t *testing.T) {
	client := Client{systemctlPath: "/usr/bin/systemctl", runner: &fakeRunner{output: []byte(
		"LoadState=not-found\nActiveState=inactive\nSubState=dead\nResult=success\n",
	)}}
	workers, err := client.Collect(context.Background(), []models.WorkerTarget{{Unit: "missing.service"}})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(workers) != 1 || workers[0].LoadState != "not-found" || workers[0].ActiveState != "inactive" || workers[0].SubState != "dead" {
		t.Fatalf("missing unit observation = %+v, want not-found/inactive/dead", workers)
	}
}

func TestCollectReturnsCommandError(t *testing.T) {
	client := Client{systemctlPath: "/usr/bin/systemctl", runner: &fakeRunner{err: errors.New("command failed")}}
	workers, err := client.Collect(context.Background(), []models.WorkerTarget{{Unit: "jobs.service"}})
	if err != nil || len(workers) != 1 || !strings.Contains(workers[0].CollectionError, "command failed") {
		t.Fatalf("Collect() = (%+v, %v), want one per-target command error", workers, err)
	}
}

func TestCollectRejectsOptionLikeUnitWithoutRunningCommand(t *testing.T) {
	runner := &fakeRunner{}
	client := Client{systemctlPath: "/usr/bin/systemctl", runner: runner}
	if _, err := client.Collect(context.Background(), []models.WorkerTarget{{Unit: "--help"}}); err == nil {
		t.Fatal("Collect() error = nil, want invalid unit error")
	}
	if runner.args != nil {
		t.Fatalf("systemctl arguments = %v, want no command for invalid unit", runner.args)
	}
}

type concurrencyRunner struct {
	mu     sync.Mutex
	active int
	peak   int
	calls  int
}

func (r *concurrencyRunner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	if name != "/usr/bin/systemctl" {
		return nil, errors.New("unexpected command")
	}
	r.mu.Lock()
	r.active++
	if r.active > r.peak {
		r.peak = r.active
	}
	r.mu.Unlock()

	time.Sleep(time.Millisecond)

	r.mu.Lock()
	r.active--
	r.calls++
	r.mu.Unlock()
	return []byte("LoadState=loaded\nActiveState=active\n"), nil
}

func TestCollectBoundsCommandConcurrency(t *testing.T) {
	const targetCount = 40
	targets := make([]models.WorkerTarget, targetCount)
	for index := range targets {
		targets[index] = models.WorkerTarget{Unit: "worker-" + strconv.Itoa(index) + ".service"}
	}
	runner := &concurrencyRunner{}
	client := Client{systemctlPath: "/usr/bin/systemctl", runner: runner}

	workers, err := client.Collect(context.Background(), targets)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(workers) != targetCount {
		t.Fatalf("Collect() returned %d workers, want %d", len(workers), targetCount)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.calls != targetCount {
		t.Fatalf("command calls = %d, want %d", runner.calls, targetCount)
	}
	if runner.peak > maxConcurrency {
		t.Fatalf("peak concurrent commands = %d, want at most %d", runner.peak, maxConcurrency)
	}
}

func TestCollectRemoteUsesPinnedReadOnlySSHOptions(t *testing.T) {
	runner := &fakeRunner{output: []byte("LoadState=loaded\nActiveState=active\n")}
	client := Client{
		sshPath:     "/usr/bin/ssh",
		sshIdentity: "/keys/monitoring key",
		knownHosts:  "/keys/known_hosts",
		runner:      runner,
	}
	workers, err := client.Collect(context.Background(), []models.WorkerTarget{{Host: "ops@worker-a", Unit: "jobs.service"}})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(workers) != 1 || workers[0].Host != "ops@worker-a" || workers[0].ActiveState != "active" {
		t.Fatalf("remote observation = %+v", workers)
	}
	joined := strings.Join(runner.args, "\n")
	for _, required := range []string{
		"/keys/monitoring key", "StrictHostKeyChecking=yes", "UserKnownHostsFile=/keys/known_hosts",
		"GlobalKnownHostsFile=/dev/null", "IdentitiesOnly=yes", "PasswordAuthentication=no",
		"ForwardAgent=no", "ClearAllForwardings=yes", "PermitLocalCommand=no", "ops@worker-a",
		"/usr/bin/systemctl show --no-pager --property=LoadState,ActiveState,SubState,Result,ExecMainCode,ExecMainStatus,StatusText -- jobs.service",
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("SSH arguments do not include %q: %v", required, runner.args)
		}
	}
	if len(runner.args) < 2 || runner.args[0] != "-F" || runner.args[1] != "/dev/null" || runner.args[len(runner.args)-2] != "ops@worker-a" || runner.args[len(runner.args)-1] != "/usr/bin/systemctl show --no-pager --property=LoadState,ActiveState,SubState,Result,ExecMainCode,ExecMainStatus,StatusText -- jobs.service" {
		t.Fatalf("SSH command tail = %v, want explicit host and fixed remote command", runner.args)
	}
}

func TestCollectRejectsRemoteShellSyntaxBeforeRunningSSH(t *testing.T) {
	runner := &fakeRunner{}
	client := Client{sshPath: "/usr/bin/ssh", sshIdentity: "/key", knownHosts: "/known_hosts", runner: runner}
	for _, target := range []models.WorkerTarget{
		{Host: "worker;touch /tmp/pwned", Unit: "jobs.service"},
		{Host: "worker-a", Unit: "jobs.service;id"},
	} {
		if _, err := client.Collect(context.Background(), []models.WorkerTarget{target}); err == nil {
			t.Errorf("Collect(%+v) error = nil, want validation error", target)
		}
	}
	if runner.args != nil {
		t.Fatalf("SSH arguments = %v, want no command for invalid targets", runner.args)
	}
}
