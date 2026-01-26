package processor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogProcessor_Process(t *testing.T) {
	processor := NewLogProcessor()

	tests := []struct {
		name     string
		input    map[string]any
		source   string
		wantLine string
	}{
		{
			name: "simple log with message",
			input: map[string]any{
				"message": "Test log message",
				"level":   "info",
			},
			source:   "test-client",
			wantLine: "Test log message",
		},
		{
			name: "log with timestamp",
			input: map[string]any{
				"timestamp": "2026-01-26T10:30:00Z",
				"message":   "Time-stamped message",
			},
			source:   "test-client",
			wantLine: "Time-stamped message",
		},
		{
			name: "log with error",
			input: map[string]any{
				"error": "Connection failed",
			},
			source:   "test-client",
			wantLine: "error: Connection failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := processor.Process(tt.input, tt.source)

			require.NotNil(t, entry, "Process should not return nil")
			assert.Equal(t, tt.wantLine, entry.Line, "Line should match expected value")
			assert.Equal(t, tt.source, entry.Labels["source"], "Source label should match")
			assert.False(t, entry.Timestamp.IsZero(), "Timestamp should not be zero")
		})
	}
}

func TestLogProcessor_ParseTimestamp(t *testing.T) {
	processor := NewLogProcessor()

	tests := []struct {
		name  string
		input any
		want  bool // whether timestamp should be valid (not zero)
	}{
		{
			name:  "RFC3339 string",
			input: "2026-01-26T10:30:00Z",
			want:  true,
		},
		{
			name:  "Unix timestamp",
			input: int64(1706267400),
			want:  true,
		},
		{
			name:  "Invalid string",
			input: "invalid",
			want:  true, // parseTimestamp returns time.Now() for invalid inputs
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.parseTimestamp(tt.input)
			isValid := !result.IsZero() && result.Before(time.Now().Add(time.Hour))

			assert.Equal(t, tt.want, isValid, "Timestamp validity should match expected value")
		})
	}
}

func TestLogProcessor_Validate(t *testing.T) {
	processor := NewLogProcessor()

	tests := []struct {
		name    string
		entry   *LogEntry
		wantErr bool
	}{
		{
			name: "valid entry",
			entry: &LogEntry{
				Labels:    map[string]string{"test": "value"},
				Timestamp: time.Now(),
				Line:      "Test message",
			},
			wantErr: false,
		},
		{
			name:    "nil entry",
			entry:   nil,
			wantErr: true,
		},
		{
			name: "empty line",
			entry: &LogEntry{
				Labels:    map[string]string{"test": "value"},
				Timestamp: time.Now(),
				Line:      "",
			},
			wantErr: true,
		},
		{
			name: "zero timestamp",
			entry: &LogEntry{
				Labels:    map[string]string{"test": "value"},
				Timestamp: time.Time{},
				Line:      "Test message",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := processor.Validate(tt.entry)
			if tt.wantErr {
				assert.Error(t, err, "Validate should return an error")
			} else {
				assert.NoError(t, err, "Validate should not return an error")
			}
		})
	}
}
