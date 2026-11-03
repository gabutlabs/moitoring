package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"moitoring/internal/collector/rabbitmq"
	"moitoring/internal/collector/systemd"
	"moitoring/internal/config"
	"moitoring/internal/models"
	"moitoring/internal/notification/email"
	"moitoring/internal/repository/sqlite"
	monitoringservice "moitoring/internal/services/monitoring"
)

func newWorkerCommand() *cobra.Command {
	configPath := defaultConfigPath()
	command := &cobra.Command{
		Use:   "worker",
		Short: "Collect monitoring data and send alerts",
		RunE: func(command *cobra.Command, _ []string) error {
			settings, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if err := settings.ValidateWorker(); err != nil {
				return err
			}
			return runWorker(command.Context(), settings)
		},
	}
	command.Flags().StringVar(&configPath, "config", configPath, "Path to the INI configuration file")
	return command
}

func runWorker(parent context.Context, settings config.Config) error {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sqlite.Open(ctx, settings.Database.Path)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer db.Close()

	collector, err := rabbitmq.NewClient(settings.RabbitMQ.URL, settings.RabbitMQ.Username, settings.RabbitMQ.Password, settings.RabbitMQ.VirtualHosts)
	if err != nil {
		return fmt.Errorf("configure RabbitMQ collector: %w", err)
	}
	var workerCollector monitoringservice.WorkerCollector
	workers := make([]models.WorkerTarget, 0, len(settings.Systemd.Units)+len(settings.Systemd.Targets))
	for _, unit := range settings.Systemd.Units {
		workers = append(workers, models.WorkerTarget{Host: "localhost", Unit: unit, ExpectedState: "active"})
	}
	for _, target := range settings.Systemd.Targets {
		workers = append(workers, models.WorkerTarget{Host: target.Host, Unit: target.Unit, ExpectedState: target.ExpectedState})
	}
	if len(workers) > 0 {
		workerCollector, err = systemd.NewClient(workers, settings.Systemd.SSHIdentityFile, settings.Systemd.SSHKnownHosts)
		if err != nil {
			return fmt.Errorf("configure systemd collector: %w", err)
		}
	}
	store := sqlite.NewMonitoringRepository(db)
	service := monitoringservice.NewService(collector, store, email.NewSender(settings.SMTP), monitoringservice.Settings{
		PollInterval:     settings.Monitoring.PollInterval,
		HistoryRetention: settings.RabbitMQ.HistoryRetention,
		ReminderInterval: settings.Alerts.ReminderInterval,
		Recipients:       settings.Alerts.Recipients,
		ExpectedQueues:   configuredQueueKeys(settings.RabbitMQ.VirtualHosts, settings.RabbitMQ.ExpectedQueues),
		ExcludedQueues:   settings.RabbitMQ.ExcludedQueues,
		WorkerCollector:  workerCollector,
		Workers:          workers,
	})
	if err := service.Run(ctx); err != nil && ctx.Err() == nil {
		return fmt.Errorf("run monitoring worker: %w", err)
	}
	return nil
}

func configuredQueueKeys(virtualHosts, queueNames []string) []models.QueueKey {
	keys := make([]models.QueueKey, 0, len(queueNames)*len(virtualHosts))
	for _, vhost := range virtualHosts {
		for _, name := range queueNames {
			keys = append(keys, models.QueueKey{VHost: vhost, Name: name})
		}
	}
	return keys
}
