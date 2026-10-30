package systemd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"moitoring/internal/models"
)

const (
	commandTimeout = 10 * time.Second
	maxConcurrency = 4
)

var (
	unitPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]*\.service$`)
	hostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9][A-Za-z0-9._-]*@)?[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

type Client struct {
	systemctlPath string
	sshPath       string
	sshIdentity   string
	knownHosts    string
	runner        commandRunner
}

func NewClient(targets []models.WorkerTarget, sshIdentity, knownHosts string) (*Client, error) {
	client := &Client{runner: execRunner{}}
	needsLocal, needsRemote := false, false
	for _, target := range targets {
		if isLocalHost(target.Host) {
			needsLocal = true
		} else {
			needsRemote = true
		}
	}
	if needsLocal {
		if runtime.GOOS != "linux" {
			return nil, errors.New("local systemd collection requires Linux; configure a remote Linux host on macOS")
		}
		path, err := findSystemctl()
		if err != nil {
			return nil, err
		}
		client.systemctlPath = path
	}
	if needsRemote {
		if !filepath.IsAbs(sshIdentity) || !filepath.IsAbs(knownHosts) {
			return nil, errors.New("remote systemd collection requires absolute SSH identity and known-hosts paths")
		}
		path, err := findSSH()
		if err != nil {
			return nil, err
		}
		client.sshPath = path
		client.sshIdentity = sshIdentity
		client.knownHosts = knownHosts
	}
	return client, nil
}

func findSystemctl() (string, error) {
	for _, path := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("systemctl was not found in /usr/bin or /bin")
}

func findSSH() (string, error) {
	for _, path := range []string{"/usr/bin/ssh", "/bin/ssh"} {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("ssh was not found in /usr/bin or /bin")
}

func (c *Client) Collect(ctx context.Context, targets []models.WorkerTarget) ([]models.WorkerObservation, error) {
	if len(targets) == 0 {
		return []models.WorkerObservation{}, nil
	}
	if c == nil || c.runner == nil {
		return nil, errors.New("systemd command runner is not configured")
	}
	for _, target := range targets {
		if !unitPattern.MatchString(strings.TrimSpace(target.Unit)) {
			return nil, fmt.Errorf("invalid systemd unit name %q", target.Unit)
		}
		if !isLocalHost(target.Host) && !hostPattern.MatchString(strings.TrimSpace(target.Host)) {
			return nil, fmt.Errorf("invalid systemd target host %q", target.Host)
		}
	}

	workers := make([]models.WorkerObservation, len(targets))
	errorsByIndex := make([]error, len(targets))
	jobIndexes := make(chan int)
	workerCount := maxConcurrency
	if len(targets) < workerCount {
		workerCount = len(targets)
	}
	var group sync.WaitGroup
	for range workerCount {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobIndexes {
				workers[index], errorsByIndex[index] = c.collectOne(ctx, targets[index])
				if errorsByIndex[index] != nil {
					workers[index].CollectionError = errorsByIndex[index].Error()
				}
			}
		}()
	}
	for index := range targets {
		select {
		case jobIndexes <- index:
		case <-ctx.Done():
			errorsByIndex[index] = ctx.Err()
		}
	}
	close(jobIndexes)
	group.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return workers, nil
}

func (c *Client) collectOne(ctx context.Context, target models.WorkerTarget) (models.WorkerObservation, error) {
	unit := strings.TrimSpace(target.Unit)
	expected := strings.TrimSpace(target.ExpectedState)
	if expected == "" {
		expected = "active"
	}
	host := strings.TrimSpace(target.Host)
	if host == "" {
		host = "localhost"
	}
	observation := models.WorkerObservation{Host: host, Unit: unit, ExpectedState: expected}

	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	propertyArg := "--property=LoadState,ActiveState,SubState,Result,ExecMainCode,ExecMainStatus,StatusText"
	var output []byte
	var err error
	if isLocalHost(host) {
		if c.systemctlPath == "" {
			return observation, errors.New("local systemd command is not configured")
		}
		output, err = c.runner.Run(commandCtx, c.systemctlPath, "show", "--no-pager", propertyArg, "--", unit)
	} else {
		if c.sshPath == "" || c.sshIdentity == "" || c.knownHosts == "" {
			return observation, errors.New("remote SSH settings are not configured")
		}
		remoteCommand := "/usr/bin/systemctl show --no-pager " + propertyArg + " -- " + unit
		args := []string{
			"-F", "/dev/null", "-T", "-i", c.sshIdentity,
			"-o", "BatchMode=yes",
			"-o", "IdentitiesOnly=yes",
			"-o", "PreferredAuthentications=publickey",
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
			"-o", "StrictHostKeyChecking=yes",
			"-o", "UpdateHostKeys=no",
			"-o", "UserKnownHostsFile=" + c.knownHosts,
			"-o", "GlobalKnownHostsFile=/dev/null",
			"-o", "ForwardAgent=no",
			"-o", "ClearAllForwardings=yes",
			"-o", "PermitLocalCommand=no",
			"-o", "RequestTTY=no",
			"-o", "ConnectTimeout=5",
			host, remoteCommand,
		}
		output, err = c.runner.Run(commandCtx, c.sshPath, args...)
	}
	if err != nil {
		return observation, fmt.Errorf("read systemd properties: %w", err)
	}
	properties := parseProperties(output)
	if _, ok := properties["ActiveState"]; !ok {
		return observation, errors.New("systemd returned no ActiveState")
	}
	observation.LoadState = properties["LoadState"]
	observation.ActiveState = properties["ActiveState"]
	observation.SubState = properties["SubState"]
	observation.Result = properties["Result"]
	observation.ExecMainCode = properties["ExecMainCode"]
	observation.ExecMainStatus = properties["ExecMainStatus"]
	observation.StatusText = properties["StatusText"]
	return observation, nil
}

func isLocalHost(host string) bool {
	host = strings.TrimSpace(host)
	return host == "" || host == "localhost"
}

func parseProperties(output []byte) map[string]string {
	properties := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			properties[key] = value
		}
	}
	return properties
}
