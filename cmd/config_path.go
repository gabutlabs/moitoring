package main

import (
	"os"
	"path/filepath"
)

func defaultConfigPath() string {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "config.ini"
	}
	return projectConfigPath(workingDirectory)
}

func projectConfigPath(startDirectory string) string {
	directory, err := filepath.Abs(startDirectory)
	if err != nil {
		return "config.ini"
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "config.ini.example")); err == nil {
			return filepath.Join(directory, "config.ini")
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "config.ini"
		}
		directory = parent
	}
}
