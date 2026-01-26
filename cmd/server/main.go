package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Norgate-AV/loki-ingest/internal/config"
	"github.com/Norgate-AV/loki-ingest/internal/loki"
	"github.com/Norgate-AV/loki-ingest/internal/server"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Configure logger
	setupLogger(cfg)

	log.Info().
		Str("version", "1.0.0").
		Str("port", cfg.ServerPort).
		Msg("Starting WebSocket Logger service")

	// Initialize Loki client
	lokiClient := loki.NewClient(cfg)

	// Initialize WebSocket server
	wsServer := server.NewWebSocketServer(cfg, lokiClient)

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsServer.HandleWebSocket)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ready", wsServer.HandleReady)

	httpServer := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Info().Msgf("Server listening on port %s", cfg.ServerPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start HTTP server")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop accepting new connections and close WebSocket connections
	wsServer.Shutdown(ctx)

	// Shutdown HTTP server
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server forced to shutdown")
	}

	// Close Loki client
	lokiClient.Close()

	log.Info().Msg("Server exited")
}

func setupLogger(cfg *config.Config) {
	// Set log level
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(level)

	// Pretty print for development
	if cfg.Environment == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
