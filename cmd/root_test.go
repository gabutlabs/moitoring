package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeRejectsInvalidPort(t *testing.T) {
	for _, port := range []string{"0", "65536"} {
		t.Run(port, func(t *testing.T) {
			command := newRootCommand()
			command.SetArgs([]string{"serve", "--config", "../config.ini.example", "--port", port})

			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), "port must be between 1 and 65535") {
				t.Fatalf("Execute() error = %v, want invalid port error", err)
			}
		})
	}
}

func TestRootCommandRegistersWorker(t *testing.T) {
	command, _, err := newRootCommand().Find([]string{"worker"})
	if err != nil {
		t.Fatalf("Find(worker) error = %v", err)
	}
	if command.Use != "worker" {
		t.Fatalf("worker command = %q, want worker", command.Use)
	}
}

func TestProjectConfigPathFindsProjectRootFromNestedDirectory(t *testing.T) {
	project := t.TempDir()
	commandDirectory := filepath.Join(project, "cmd", "nested")
	if err := os.MkdirAll(commandDirectory, 0o755); err != nil {
		t.Fatalf("create nested command directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, "config.ini.example"), nil, 0o600); err != nil {
		t.Fatalf("create example marker: %v", err)
	}

	got := projectConfigPath(commandDirectory)
	want := filepath.Join(project, "config.ini")
	if got != want {
		t.Fatalf("projectConfigPath() = %q, want %q", got, want)
	}
}
