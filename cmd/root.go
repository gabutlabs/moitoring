package main

import "github.com/spf13/cobra"

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:          "moitoring",
		Short:        "Monitor systemd workers and RabbitMQ queues",
		SilenceUsage: true,
	}
	root.AddCommand(newServeCommand(), newWorkerCommand())
	return root
}
