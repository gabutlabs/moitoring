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

func TestRootCommandRegistersTUI(t *testing.T) {
	root := newRootCommand()
	if root.RunE == nil {
		t.Fatal("root RunE is nil, want bare command to launch the TUI")
	}

	command, _, err := root.Find([]string{"tui"})
	if err != nil {
		t.Fatalf("Find(tui) error = %v", err)
	}
	if command.Use != "tui" {
		t.Fatalf("tui command = %q, want %q", command.Use, "tui")
	}
}

func TestResolveDefaultConfigPathUsesInstalledConfigFromSymlink(t *testing.T) {
	installDir := t.TempDir()
	binDir := filepath.Join(installDir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	installedBinary := filepath.Join(installDir, "moitoring")
	if err := os.WriteFile(installedBinary, nil, 0o755); err != nil {
		t.Fatalf("create installed binary: %v", err)
	}
	configPath := filepath.Join(installDir, "config.ini")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatalf("create installed config: %v", err)
	}
	commandPath := filepath.Join(binDir, "moitoring")
	if err := os.Symlink(installedBinary, commandPath); err != nil {
		t.Fatalf("create command symlink: %v", err)
	}

	got := resolveDefaultConfigPath(t.TempDir(), commandPath)
	want, err := filepath.EvalSymlinks(configPath)
	if err != nil {
		t.Fatalf("resolve config symlinks: %v", err)
	}
	if got != want {
		t.Fatalf("resolveDefaultConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveDatabasePathUsesConfigDirectory(t *testing.T) {
	configDir := t.TempDir()
	got := resolveDatabasePath(filepath.Join(configDir, "config.ini"), "./data/moitoring.db")
	want := filepath.Join(configDir, "data", "moitoring.db")
	if got != want {
		t.Fatalf("resolveDatabasePath() = %q, want %q", got, want)
	}
}

func TestResolveDatabasePathPreservesAbsolutePath(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "moitoring.db")
	if got := resolveDatabasePath("config.ini", databasePath); got != databasePath {
		t.Fatalf("resolveDatabasePath() = %q, want %q", got, databasePath)
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
