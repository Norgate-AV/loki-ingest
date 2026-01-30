package processor

import (
	"fmt"
	"strings"
	"time"
)

// ControlSystemLogEntry represents a log entry from control system processors (NetLinx, Crestron, etc.)
type ControlSystemLogEntry struct {
	ID              string `json:"id"`
	Timestamp       string `json:"timestamp"`
	ClientID        string `json:"clientId"`
	HostName        string `json:"hostName"`
	RoomName        string `json:"roomName"`
	Level           string `json:"level"`
	Message         string `json:"message"`
	SystemType      string `json:"systemType,omitempty"`
	FirmwareVersion string `json:"firmwareVersion,omitempty"`
	IPAddress       string `json:"ipAddress,omitempty"`
}

// ProcessControlSystem processes a control system log entry into a structured format for Loki
func (p *LogProcessor) ProcessControlSystem(entry *ControlSystemLogEntry, source string) *LogEntry {
	logEntry := &LogEntry{
		Labels:    make(map[string]string),
		Timestamp: time.Now(),
		Line:      "",
	}

	// Parse timestamp
	if entry.Timestamp != "" {
		logEntry.Timestamp = p.parseTimestamp(entry.Timestamp)
	}

	// Set labels from control system fields
	logEntry.Labels["source"] = source

	// Set job label based on hostname or room to create separate log streams per system
	// This overrides any default job label and ensures logs are grouped by system
	if entry.HostName != "" {
		logEntry.Labels["job"] = entry.HostName
		logEntry.Labels["host"] = entry.HostName
	} else if entry.RoomName != "" {
		logEntry.Labels["job"] = entry.RoomName
		logEntry.Labels["room"] = entry.RoomName
	}

	if entry.Level != "" {
		logEntry.Labels["level"] = strings.ToLower(entry.Level)
	}

	if entry.ClientID != "" {
		logEntry.Labels["client"] = entry.ClientID
	}

	// Set room label if not already used for job
	if entry.RoomName != "" && logEntry.Labels["job"] != entry.RoomName {
		logEntry.Labels["room"] = entry.RoomName
	}

	if entry.SystemType != "" {
		logEntry.Labels["system_type"] = entry.SystemType
	}

	if entry.FirmwareVersion != "" {
		logEntry.Labels["firmware_version"] = entry.FirmwareVersion
	}

	if entry.IPAddress != "" {
		logEntry.Labels["ip_address"] = entry.IPAddress
	}

	// Build the log line
	logEntry.Line = p.buildControlSystemLogLine(entry)

	return logEntry
}

// buildControlSystemLogLine constructs the log line from control system entry
func (p *LogProcessor) buildControlSystemLogLine(entry *ControlSystemLogEntry) string {
	// Use the message if present
	if entry.Message != "" {
		return entry.Message
	}

	// Fallback to a generic representation
	return fmt.Sprintf("Control system log: client=%s, room=%s", entry.ClientID, entry.RoomName)
}
