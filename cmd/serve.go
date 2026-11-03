package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"moitoring/internal/config"
	"moitoring/internal/handler"
	"moitoring/internal/repository/sqlite"
	authservice "moitoring/internal/services/auth"
	monitoringservice "moitoring/internal/services/monitoring"
	webassets "moitoring/internal/web"
)

func newServeCommand() *cobra.Command {
	var port int
	configPath := defaultConfigPath()
	command := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API",
		RunE: func(command *cobra.Command, _ []string) error {
			settings, err := config.Load(configPath)
			if err != nil {
				return err
			}

			if command.Flags().Changed("port") {
				settings.Server.Port = port
			}
			if settings.Server.Port < 1 || settings.Server.Port > 65535 {
				return fmt.Errorf("port must be between 1 and 65535: %d", settings.Server.Port)
			}

			return runServer(settings, configPath)
		},
	}
	command.Flags().IntVarP(&port, "port", "p", 0, "Override the configured HTTP port")
	command.Flags().StringVar(&configPath, "config", configPath, "Path to the INI configuration file")
	return command
}

func runServer(settings config.Config, configPath string) error {
	db, err := sqlite.Open(context.Background(), settings.Database.Path)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer db.Close()

	created, err := sqlite.EnsureAdmin(context.Background(), db, settings.Admin.Username, settings.Admin.Email, settings.Admin.Password)
	if err != nil {
		return fmt.Errorf("initialize admin user: %w", err)
	}
	if created {
		log.Printf("created admin user %q", settings.Admin.Username)
	} else {
		log.Printf("admin user %q already exists; skipped", settings.Admin.Username)
	}

	authService := authservice.New(
		sqlite.NewAuthRepository(db),
		settings.Admin.Email,
		settings.Admin.RequiresPasswordChange(),
		func() error { return config.CompleteAdminPasswordChange(configPath) },
	)
	authHandler := handler.NewAuthHandler(authService, settings.Admin.Email, settings.Server.SecureCookies)
	monitoringHandler := handler.NewMonitoringHandler(monitoringservice.NewQueryService(
		sqlite.NewMonitoringRepository(db), settings.Monitoring.PollInterval, settings.RabbitMQ.ExcludedQueues,
		configuredQueueKeys(settings.RabbitMQ.VirtualHosts, settings.RabbitMQ.ExpectedQueues),
	))

	router := gin.New()
	if err := router.SetTrustedProxies(settings.Server.TrustedProxies); err != nil {
		return fmt.Errorf("configure trusted proxies: %w", err)
	}
	router.Use(gin.Logger(), gin.Recovery())
	api := router.Group("/api")
	api.Use(authHandler.Middleware())
	authHandler.Register(api)
	monitoringHandler.Register(api)
	api.GET("/health", handler.Health)
	registerWebUI(router, settings.Server.WebUIEnabled)

	server := &http.Server{
		Addr:              settings.Server.Address(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func registerWebUI(router *gin.Engine, enabled bool) {
	if enabled {
		router.NoRoute(gin.WrapH(webassets.Handler()))
	}
}
