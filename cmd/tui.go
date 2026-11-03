package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"moitoring/internal/config"
	"moitoring/internal/repository/sqlite"
	monitoringservice "moitoring/internal/services/monitoring"
	"moitoring/internal/tui"
)

func newTUICommand() *cobra.Command {
	configPath := defaultConfigPath()
	command := &cobra.Command{
		Use:   "tui",
		Short: "Open the read-only terminal dashboard",
		RunE: func(command *cobra.Command, _ []string) error {
			return runTUI(command, configPath)
		},
	}
	command.Flags().StringVar(&configPath, "config", configPath, "Path to the INI configuration file")
	return command
}

func runTUI(command *cobra.Command, configPath string) error {
	settings, err := config.Load(configPath)
	if err != nil {
		return err
	}

	db, err := sqlite.Open(command.Context(), resolveDatabasePath(configPath, settings.Database.Path))
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer db.Close()

	reader := monitoringservice.NewQueryService(
		sqlite.NewMonitoringRepository(db), settings.Monitoring.PollInterval, settings.RabbitMQ.ExcludedQueues,
		configuredQueueKeys(settings.RabbitMQ.VirtualHosts, settings.RabbitMQ.ExpectedQueues),
	)
	return tui.Run(command.Context(), reader, settings.Monitoring.PollInterval)
}

func resolveDatabasePath(configPath, databasePath string) string {
	if filepath.IsAbs(databasePath) {
		return databasePath
	}
	absoluteConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return filepath.Clean(databasePath)
	}
	return filepath.Join(filepath.Dir(absoluteConfigPath), databasePath)
}
