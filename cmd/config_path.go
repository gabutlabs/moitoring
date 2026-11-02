package main

import (
	"os"
	"path/filepath"
)

func defaultConfigPath() string {
	workingDirectory, _ := os.Getwd()
	executablePath, _ := os.Executable()
	return resolveDefaultConfigPath(workingDirectory, executablePath)
}

func resolveDefaultConfigPath(workingDirectory, executablePath string) string {
	if projectPath, found := findProjectConfigPath(workingDirectory); found {
		return projectPath
	}
	if executablePath != "" {
		if resolvedPath, err := filepath.EvalSymlinks(executablePath); err == nil {
			executablePath = resolvedPath
		}
		installedPath := filepath.Join(filepath.Dir(executablePath), "config.ini")
		if info, err := os.Stat(installedPath); err == nil && info.Mode().IsRegular() {
			return installedPath
		}
	}
	return "config.ini"
}

func projectConfigPath(startDirectory string) string {
	if path, found := findProjectConfigPath(startDirectory); found {
		return path
	}
	return "config.ini"
}

func findProjectConfigPath(startDirectory string) (string, bool) {
	if startDirectory == "" {
		return "", false
	}
	directory, err := filepath.Abs(startDirectory)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "config.ini.example")); err == nil {
			return filepath.Join(directory, "config.ini"), true
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", false
		}
		directory = parent
	}
}
