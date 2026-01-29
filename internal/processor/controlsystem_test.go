package processor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogProcessor_ProcessControlSystem(t *testing.T) {
	processor := NewLogProcessor()

	tests := []struct {
		name        string
		entry       *ControlSystemLogEntry
		source      string
		wantLine    string
		wantLabels  map[string]string
		shouldError bool
	}{
		{
			name: "complete control system entry with UUID",
			entry: &ControlSystemLogEntry{
				ID:        "550e8400-e29b-41d4-a716-446655440000",
				Timestamp: "2026-01-26T10:30:00Z",
				ClientID:  "processor-01",
				HostName:  "control-room-1",
				RoomName:  "conference-a",
				Level:     "info",
				Message:   "System initialized successfully",
			},
			source:   "192.168.1.100",
			wantLine: "System initialized successfully",
			wantLabels: map[string]string{
				"source": "192.168.1.100",
				"level":  "info",
				"host":   "control-room-1",
				"client": "processor-01",
				"room":   "conference-a",
			},
		},
		{
			name: "entry without UUID",
			entry: &ControlSystemLogEntry{
				Timestamp: "2026-01-26T10:30:00Z",
				ClientID:  "processor-02",
				HostName:  "control-room-2",
				RoomName:  "lobby",
				Level:     "warn",
				Message:   "Temperature threshold exceeded",
			},
			source:   "192.168.1.101",
			wantLine: "Temperature threshold exceeded",
			wantLabels: map[string]string{
				"source": "192.168.1.101",
				"level":  "warn",
				"host":   "control-room-2",
				"client": "processor-02",
				"room":   "lobby",
			},
		},
		{
			name: "entry with system context fields",
			entry: &ControlSystemLogEntry{
				ID:              "123e4567-e89b-12d3-a456-426614174000",
				Timestamp:       "2026-01-26T10:30:00Z",
				ClientID:        "crestron-main",
				HostName:        "cp4-server",
				RoomName:        "auditorium",
				Level:           "error",
				Message:         "Connection timeout",
				SystemType:      "Crestron",
				FirmwareVersion: "2.9001.00047",
				IPAddress:       "10.0.0.50",
			},
			source:   "10.0.0.50",
			wantLine: "Connection timeout",
			wantLabels: map[string]string{
				"source":           "10.0.0.50",
				"level":            "error",
				"host":             "cp4-server",
				"client":           "crestron-main",
				"room":             "auditorium",
				"system_type":      "Crestron",
				"firmware_version": "2.9001.00047",
				"ip_address":       "10.0.0.50",
			},
		},
		{
			name: "entry without message uses fallback format",
			entry: &ControlSystemLogEntry{
				ID:       "test-uuid-123",
				ClientID: "netlinx-01",
				RoomName: "room-101",
				Level:    "debug",
			},
			source:   "test-source",
			wantLine: "Control system log: client=netlinx-01, room=room-101",
			wantLabels: map[string]string{
				"source": "test-source",
				"level":  "debug",
				"client": "netlinx-01",
				"room":   "room-101",
			},
		},
		{
			name: "minimal entry",
			entry: &ControlSystemLogEntry{
				ClientID: "minimal-client",
				Message:  "Simple log",
			},
			source:   "minimal-source",
			wantLine: "Simple log",
			wantLabels: map[string]string{
				"source": "minimal-source",
				"client": "minimal-client",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logEntry := processor.ProcessControlSystem(tt.entry, tt.source)

			require.NotNil(t, logEntry, "ProcessControlSystem should not return nil")
			assert.Equal(t, tt.wantLine, logEntry.Line, "Log line should match expected")

			// Check all expected labels are present and match
			for key, expectedValue := range tt.wantLabels {
				actualValue, exists := logEntry.Labels[key]
				assert.True(t, exists, "Label %s should exist", key)
				assert.Equal(t, expectedValue, actualValue, "Label %s should match", key)
			}

			assert.False(t, logEntry.Timestamp.IsZero(), "Timestamp should not be zero")

			// Verify log_id is NOT in labels (to avoid high cardinality)
			_, hasLogID := logEntry.Labels["log_id"]
			assert.False(t, hasLogID, "log_id should not be in labels to avoid high cardinality")
		})
	}
}

func TestControlSystemLogEntry_Validation(t *testing.T) {
	tests := []struct {
		name  string
		entry *ControlSystemLogEntry
		valid bool
	}{
		{
			name: "valid entry",
			entry: &ControlSystemLogEntry{
				ClientID: "test-client",
				Message:  "test message",
			},
			valid: true,
		},
		{
			name: "valid entry with all fields",
			entry: &ControlSystemLogEntry{
				ID:              "uuid-123",
				Timestamp:       "2026-01-26T10:30:00Z",
				ClientID:        "client",
				HostName:        "host",
				RoomName:        "room",
				Level:           "info",
				Message:         "message",
				SystemType:      "Crestron",
				FirmwareVersion: "1.0.0",
				IPAddress:       "192.168.1.1",
			},
			valid: true,
		},
		{
			name: "entry with only UUID and message",
			entry: &ControlSystemLogEntry{
				ID:      "uuid-456",
				Message: "message only",
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the entry can be processed without panic
			processor := NewLogProcessor()
			result := processor.ProcessControlSystem(tt.entry, "test-source")

			if tt.valid {
				assert.NotNil(t, result, "Should process valid entry")
				assert.NotEmpty(t, result.Line, "Should have a log line")
			}
		})
	}
}

func TestControlSystemTimestampParsing(t *testing.T) {
	processor := NewLogProcessor()

	tests := []struct {
		name           string
		entry          *ControlSystemLogEntry
		expectRecent   bool
		expectSpecific *time.Time
	}{
		{
			name: "RFC3339 timestamp",
			entry: &ControlSystemLogEntry{
				Timestamp: "2026-01-26T10:30:00Z",
				ClientID:  "test",
			},
			expectSpecific: func() *time.Time {
				t, _ := time.Parse(time.RFC3339, "2026-01-26T10:30:00Z")
				return &t
			}(),
		},
		{
			name: "no timestamp uses current time",
			entry: &ControlSystemLogEntry{
				ClientID: "test",
			},
			expectRecent: true,
		},
		{
			name: "empty timestamp uses current time",
			entry: &ControlSystemLogEntry{
				Timestamp: "",
				ClientID:  "test",
			},
			expectRecent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.ProcessControlSystem(tt.entry, "test-source")
			require.NotNil(t, result)

			if tt.expectRecent {
				// Should be within last few seconds
				assert.WithinDuration(t, time.Now(), result.Timestamp, 2*time.Second,
					"Timestamp should be recent")
			}

			if tt.expectSpecific != nil {
				assert.Equal(t, tt.expectSpecific.Unix(), result.Timestamp.Unix(),
					"Timestamp should match expected value")
			}
		})
	}
}
