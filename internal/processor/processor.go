package processor

import (
	"fmt"
	"time"
)

// LogProcessor handles processing and enrichment of log entries
type LogProcessor struct{}

// NewLogProcessor creates a new log processor instance
func NewLogProcessor() *LogProcessor {
	return &LogProcessor{}
}

// LogEntry represents a processed log entry ready for Loki
type LogEntry struct {
	Labels    map[string]string
	Timestamp time.Time
	Line      string
}

// Process takes a raw log entry and processes it into a structured format
func (p *LogProcessor) Process(rawLog map[string]any, source string) *LogEntry {
	entry := &LogEntry{
		Labels:    make(map[string]string),
		Timestamp: time.Now(),
		Line:      "",
	}

	// Extract timestamp if present
	if ts, ok := rawLog["timestamp"]; ok {
		entry.Timestamp = p.parseTimestamp(ts)
	} else if ts, ok := rawLog["ts"]; ok {
		entry.Timestamp = p.parseTimestamp(ts)
	} else if ts, ok := rawLog["time"]; ok {
		entry.Timestamp = p.parseTimestamp(ts)
	} else if ts, ok := rawLog["@timestamp"]; ok {
		entry.Timestamp = p.parseTimestamp(ts)
	}

	// Extract and set labels
	entry.Labels["source"] = source

	// Extract level/severity
	if level, ok := p.extractString(rawLog, "level", "severity", "loglevel", "log_level"); ok {
		entry.Labels["level"] = level
	}

	// Extract application/service name
	if app, ok := p.extractString(rawLog, "app", "application", "service", "service_name"); ok {
		entry.Labels["app"] = app
	}

	// Extract environment
	if env, ok := p.extractString(rawLog, "environment", "env"); ok {
		entry.Labels["environment"] = env
	}

	// Extract host/hostname
	if host, ok := p.extractString(rawLog, "host", "hostname", "node"); ok {
		entry.Labels["host"] = host
	}

	// Extract container information
	if container, ok := p.extractString(rawLog, "container", "container_id", "container_name"); ok {
		entry.Labels["container"] = container
	}

	// Build the log line
	entry.Line = p.buildLogLine(rawLog)

	return entry
}

// parseTimestamp attempts to parse various timestamp formats
func (p *LogProcessor) parseTimestamp(ts any) time.Time {
	switch v := ts.(type) {
	case string:
		// Try various formats
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05.000Z07:00",
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
		}

		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				return t
			}
		}
	case float64:
		// Unix timestamp (seconds)
		return time.Unix(int64(v), 0)
	case int64:
		// Unix timestamp (seconds)
		return time.Unix(v, 0)
	case int:
		// Unix timestamp (seconds)
		return time.Unix(int64(v), 0)
	}

	// If parsing fails, return current time
	return time.Now()
}

// extractString tries to extract a string value from multiple possible keys
func (p *LogProcessor) extractString(data map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			if str, ok := value.(string); ok && str != "" {
				return str, true
			}
		}
	}

	return "", false
}

// buildLogLine constructs the actual log line from the raw log data
func (p *LogProcessor) buildLogLine(rawLog map[string]any) string {
	// If there's a 'message' or 'msg' field, use that
	if msg, ok := p.extractString(rawLog, "message", "msg", "log", "text"); ok {
		return msg
	}

	// Otherwise, build a structured line from key fields
	var line string

	// Add timestamp if present
	if ts, ok := rawLog["timestamp"]; ok {
		line += fmt.Sprintf("[%v] ", ts)
	}

	// Add level if present
	if level, ok := rawLog["level"]; ok {
		line += fmt.Sprintf("%v: ", level)
	}

	// Add message-like fields
	for _, key := range []string{"message", "msg", "text", "log"} {
		if value, ok := rawLog[key]; ok {
			line += fmt.Sprintf("%v ", value)
			break
		}
	}

	// If still empty, try to extract error information
	if line == "" {
		if err, ok := rawLog["error"]; ok {
			line = fmt.Sprintf("error: %v", err)
		} else if err, ok := rawLog["err"]; ok {
			line = fmt.Sprintf("error: %v", err)
		}
	}

	// If still empty, use a generic representation
	if line == "" {
		line = fmt.Sprintf("log entry: %v", rawLog)
	}

	return line
}

// Validate checks if a log entry is valid
func (p *LogProcessor) Validate(entry *LogEntry) error {
	if entry == nil {
		return fmt.Errorf("log entry is nil")
	}

	if entry.Line == "" {
		return fmt.Errorf("log line is empty")
	}

	if entry.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is zero")
	}

	return nil
}
