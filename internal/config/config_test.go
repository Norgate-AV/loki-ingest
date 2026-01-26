package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear environment variables to test defaults
	envVars := []string{
		"SERVER_PORT", "ENVIRONMENT", "LOG_LEVEL", "MAX_CONNECTIONS",
		"PING_INTERVAL", "PONG_WAIT", "WRITE_WAIT",
		"LOKI_URL", "LOKI_BATCH_SIZE", "LOKI_BATCH_WAIT",
		"LOKI_RETRY_ATTEMPTS", "LOKI_RETRY_WAIT", "LOKI_MAX_BACKOFF",
		"LOKI_BASIC_AUTH_USER", "LOKI_BASIC_AUTH_PASS",
		"LOKI_DEFAULT_JOB_LABEL", "LOKI_DEFAULT_INSTANCE_LABEL",
	}

	for _, envVar := range envVars {
		os.Unsetenv(envVar)
	}

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Test server defaults
	assert.Equal(t, "8080", cfg.ServerPort)
	assert.Equal(t, "production", cfg.Environment)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, 500, cfg.MaxConnections)

	// Test WebSocket defaults
	assert.Equal(t, 30*time.Second, cfg.PingInterval)
	assert.Equal(t, 60*time.Second, cfg.PongWait)
	assert.Equal(t, 10*time.Second, cfg.WriteWait)

	// Test Loki defaults
	assert.Equal(t, "http://localhost:3100", cfg.LokiURL)
	assert.Equal(t, 100, cfg.LokiBatchSize)
	assert.Equal(t, 5*time.Second, cfg.LokiBatchWait)
	assert.Equal(t, 3, cfg.LokiRetryAttempts)
	assert.Equal(t, 1*time.Second, cfg.LokiRetryWait)
	assert.Equal(t, 30*time.Second, cfg.LokiMaxBackoff)

	// Test default labels
	require.NotNil(t, cfg.LokiDefaultLabels)
	assert.Equal(t, "loki-ingest", cfg.LokiDefaultLabels["job"])
}

func TestLoad_EnvironmentVariables(t *testing.T) {
	// Set environment variables
	testEnv := map[string]string{
		"SERVER_PORT":         "9090",
		"ENVIRONMENT":         "testing",
		"LOG_LEVEL":           "debug",
		"MAX_CONNECTIONS":     "1000",
		"PING_INTERVAL":       "20s",
		"PONG_WAIT":           "25s",
		"WRITE_WAIT":          "5s",
		"LOKI_URL":            "http://loki.example.com:3100",
		"LOKI_BATCH_SIZE":     "50",
		"LOKI_BATCH_WAIT":     "10s",
		"LOKI_RETRY_ATTEMPTS": "5",
		"LOKI_RETRY_WAIT":     "2s",
		"LOKI_MAX_BACKOFF":    "60s",
	}

	for key, value := range testEnv {
		os.Setenv(key, value)
	}
	defer func() {
		for key := range testEnv {
			os.Unsetenv(key)
		}
	}()

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "9090", cfg.ServerPort)
	assert.Equal(t, "testing", cfg.Environment)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, 1000, cfg.MaxConnections)
	assert.Equal(t, 20*time.Second, cfg.PingInterval)
	assert.Equal(t, 25*time.Second, cfg.PongWait)
	assert.Equal(t, 5*time.Second, cfg.WriteWait)
	assert.Equal(t, "http://loki.example.com:3100", cfg.LokiURL)
	assert.Equal(t, 50, cfg.LokiBatchSize)
	assert.Equal(t, 10*time.Second, cfg.LokiBatchWait)
	assert.Equal(t, 5, cfg.LokiRetryAttempts)
	assert.Equal(t, 2*time.Second, cfg.LokiRetryWait)
	assert.Equal(t, 60*time.Second, cfg.LokiMaxBackoff)
}

func TestLoad_DefaultLabels(t *testing.T) {
	tests := []struct {
		name           string
		jobLabel       string
		instanceLabel  string
		expectJob      string
		expectInstance bool
	}{
		{
			name:           "custom job label",
			jobLabel:       "my-service",
			instanceLabel:  "",
			expectJob:      "my-service",
			expectInstance: false,
		},
		{
			name:           "both labels set",
			jobLabel:       "my-service",
			instanceLabel:  "prod-01",
			expectJob:      "my-service",
			expectInstance: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.jobLabel != "" {
				os.Setenv("LOKI_DEFAULT_JOB_LABEL", tt.jobLabel)
			} else {
				os.Unsetenv("LOKI_DEFAULT_JOB_LABEL")
			}

			if tt.instanceLabel != "" {
				os.Setenv("LOKI_DEFAULT_INSTANCE_LABEL", tt.instanceLabel)
			} else {
				os.Unsetenv("LOKI_DEFAULT_INSTANCE_LABEL")
			}

			defer func() {
				os.Unsetenv("LOKI_DEFAULT_JOB_LABEL")
				os.Unsetenv("LOKI_DEFAULT_INSTANCE_LABEL")
			}()

			cfg, err := Load()
			require.NoError(t, err)

			assert.Equal(t, tt.expectJob, cfg.LokiDefaultLabels["job"])

			if tt.expectInstance {
				assert.NotEmpty(t, cfg.LokiDefaultLabels["instance"])
			}
		})
	}
}

func TestConfig_Validation(t *testing.T) {
	cfg, err := Load()
	require.NoError(t, err)

	assert.Greater(t, cfg.MaxConnections, 0)
	assert.Greater(t, cfg.LokiBatchSize, 0)
	assert.Greater(t, cfg.LokiRetryAttempts, 0)
	assert.NotEmpty(t, cfg.ServerPort)
	assert.NotEmpty(t, cfg.LokiURL)
	assert.Greater(t, cfg.PongWait, cfg.PingInterval)
}
