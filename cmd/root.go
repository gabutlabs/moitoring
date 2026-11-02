package main

import "github.com/spf13/cobra"

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:          "moitoring",
		Short:        "Monitor systemd workers and RabbitMQ queues",
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			return runTUI(command, defaultConfigPath())
		},
	}
	root.AddCommand(newServeCommand(), newWorkerCommand(), newTUICommand())
	return root
}
