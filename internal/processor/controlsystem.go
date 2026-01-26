package processor

import (
	"fmt"
	"time"
)

// ControlSystemLogEntry represents a log entry from control system processors (NetLinx, Crestron, etc.)
type ControlSystemLogEntry struct {
	ID              string `json:"id"`
	Timestamp       string `json:"timestamp"`
	Client          string `json:"client"`
	Hostname        string `json:"hostname"`
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

	// Set labels from NetLinx fields
	logEntry.Labels["source"] = source

	if entry.Level != "" {
		logEntry.Labels["level"] = entry.Level
	}

	if entry.Client != "" {
		logEntry.Labels["client"] = entry.Client
	}

	if entry.Hostname != "" {
		logEntry.Labels["host"] = entry.Hostname
	}

	if entry.RoomName != "" {
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
	// Prepend UUID if present
	prefix := ""
	if entry.ID != "" {
		prefix = fmt.Sprintf("[%s] ", entry.ID)
	}

	// Use the message if present
	if entry.Message != "" {
		return prefix + entry.Message
	}

	// Fallback to a generic representation
	return fmt.Sprintf("%sControl system log: client=%s, room=%s", prefix, entry.Client, entry.RoomName)
}
