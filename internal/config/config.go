package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application
type Config struct {
	// Server configuration
	ServerPort  string
	Environment string
	LogLevel    string

	// WebSocket configuration
	MaxConnections     int
	ReadBufferSize     int
	WriteBufferSize    int
	PingInterval       time.Duration
	PongWait           time.Duration
	WriteWait          time.Duration
	MaxMessageSize     int64
	MessageChannelSize int

	// Loki configuration
	LokiURL           string
	LokiBatchSize     int
	LokiBatchWait     time.Duration
	LokiTimeout       time.Duration
	LokiRetryAttempts int
	LokiRetryWait     time.Duration
	LokiMaxBackoff    time.Duration
	LokiTenantID      string
	LokiDefaultLabels map[string]string
	LokiBasicAuthUser string
	LokiBasicAuthPass string
}

// Load reads configuration from environment variables and .env file
func Load() (*Config, error) {
	// Try to load .env file (ignore error if file doesn't exist)
	_ = godotenv.Load()

	cfg := &Config{
		// Server defaults
		ServerPort:  getEnv("SERVER_PORT", "8080"),
		Environment: getEnv("ENVIRONMENT", "production"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),

		// WebSocket defaults
		MaxConnections:     getEnvInt("MAX_CONNECTIONS", 500),
		ReadBufferSize:     getEnvInt("READ_BUFFER_SIZE", 4096),
		WriteBufferSize:    getEnvInt("WRITE_BUFFER_SIZE", 4096),
		PingInterval:       getEnvDuration("PING_INTERVAL", 30*time.Second),
		PongWait:           getEnvDuration("PONG_WAIT", 60*time.Second),
		WriteWait:          getEnvDuration("WRITE_WAIT", 10*time.Second),
		MaxMessageSize:     getEnvInt64("MAX_MESSAGE_SIZE", 1024*1024), // 1MB
		MessageChannelSize: getEnvInt("MESSAGE_CHANNEL_SIZE", 256),

		// Loki defaults
		LokiURL:           getEnv("LOKI_URL", "http://localhost:3100"),
		LokiBatchSize:     getEnvInt("LOKI_BATCH_SIZE", 100),
		LokiBatchWait:     getEnvDuration("LOKI_BATCH_WAIT", 5*time.Second),
		LokiTimeout:       getEnvDuration("LOKI_TIMEOUT", 10*time.Second),
		LokiRetryAttempts: getEnvInt("LOKI_RETRY_ATTEMPTS", 3),
		LokiRetryWait:     getEnvDuration("LOKI_RETRY_WAIT", 1*time.Second),
		LokiMaxBackoff:    getEnvDuration("LOKI_MAX_BACKOFF", 30*time.Second),
		LokiTenantID:      getEnv("LOKI_TENANT_ID", ""),
		LokiBasicAuthUser: getEnv("LOKI_BASIC_AUTH_USER", ""),
		LokiBasicAuthPass: getEnv("LOKI_BASIC_AUTH_PASS", ""),
	}

	// Parse default labels
	cfg.LokiDefaultLabels = make(map[string]string)

	if jobLabel := getEnv("LOKI_DEFAULT_JOB_LABEL", "loki-ingest"); jobLabel != "" {
		cfg.LokiDefaultLabels["job"] = jobLabel
	}

	if instanceLabel := getEnv("LOKI_DEFAULT_INSTANCE_LABEL", ""); instanceLabel != "" {
		cfg.LokiDefaultLabels["instance"] = instanceLabel
	}

	// Validate required configuration
	if cfg.LokiURL == "" {
		return nil, fmt.Errorf("LOKI_URL is required")
	}

	return cfg, nil
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return defaultValue
}

// getEnvInt retrieves an environment variable as an integer or returns a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}

	return defaultValue
}

// getEnvInt64 retrieves an environment variable as an int64 or returns a default value
func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intValue
		}
	}

	return defaultValue
}

// getEnvDuration retrieves an environment variable as a duration or returns a default value
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}

	return defaultValue
}
