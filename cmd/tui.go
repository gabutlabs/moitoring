package main

import (
	"fmt"

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
			settings, err := config.Load(configPath)
			if err != nil {
				return err
			}

			db, err := sqlite.Open(command.Context(), settings.Database.Path)
			if err != nil {
				return fmt.Errorf("initialize database: %w", err)
			}
			defer db.Close()

			reader := monitoringservice.NewQueryService(
				sqlite.NewMonitoringRepository(db), settings.Monitoring.PollInterval, settings.RabbitMQ.ExcludedQueues,
			)
			return tui.Run(command.Context(), reader, settings.Monitoring.PollInterval)
		},
	}
	command.Flags().StringVar(&configPath, "config", configPath, "Path to the INI configuration file")
	return command
}
