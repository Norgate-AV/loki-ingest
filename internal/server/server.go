package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/Norgate-AV/loki-ingest/internal/config"
	"github.com/Norgate-AV/loki-ingest/internal/loki"
	"github.com/Norgate-AV/loki-ingest/internal/processor"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// WebSocketServer manages WebSocket connections and message processing
type WebSocketServer struct {
	config     *config.Config
	lokiClient *loki.Client
	upgrader   websocket.Upgrader
	processor  *processor.LogProcessor

	// Connection management
	connections map[*websocket.Conn]bool
	connMutex   sync.RWMutex
	connCount   int32

	// Shutdown management
	shutdownChan chan struct{}
	wg           sync.WaitGroup
}

// LogResponse represents the response sent back to the client after processing a log
type LogResponse struct {
	Status    string    `json:"status"`            // "success" or "error"
	ID        string    `json:"id,omitempty"`      // Log ID if present
	Timestamp time.Time `json:"timestamp"`         // When the log was processed
	Type      string    `json:"type,omitempty"`    // Log type (control_system or generic)
	Message   string    `json:"message,omitempty"` // Error message if status is error
}

// NewWebSocketServer creates a new WebSocket server instance
func NewWebSocketServer(cfg *config.Config, lokiClient *loki.Client) *WebSocketServer {
	return &WebSocketServer{
		config:       cfg,
		lokiClient:   lokiClient,
		processor:    processor.NewLogProcessor(),
		connections:  make(map[*websocket.Conn]bool),
		shutdownChan: make(chan struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  cfg.ReadBufferSize,
			WriteBufferSize: cfg.WriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				// In production, implement proper origin checking
				return true
			},
		},
	}
}

// HandleWebSocket handles incoming WebSocket connection requests
func (s *WebSocketServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check connection limit
	s.connMutex.RLock()
	currentConnections := len(s.connections)
	s.connMutex.RUnlock()

	if currentConnections >= s.config.MaxConnections {
		log.Warn().
			Int("current", currentConnections).
			Int("max", s.config.MaxConnections).
			Msg("Connection limit reached, rejecting new connection")
		http.Error(w, "Connection limit reached", http.StatusServiceUnavailable)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to upgrade connection")
		return
	}

	// Register connection
	s.connMutex.Lock()
	s.connections[conn] = true
	connCount := len(s.connections)
	s.connMutex.Unlock()

	log.Info().
		Str("remote_addr", r.RemoteAddr).
		Int("total_connections", connCount).
		Msg("New WebSocket connection established")

	// Handle connection in a goroutine
	s.wg.Add(1)
	go s.handleConnection(conn, r.RemoteAddr)
}

// handleConnection processes messages from a WebSocket connection
func (s *WebSocketServer) handleConnection(conn *websocket.Conn, remoteAddr string) {
	defer s.wg.Done()
	defer func() {
		// Unregister connection
		s.connMutex.Lock()
		delete(s.connections, conn)
		connCount := len(s.connections)
		s.connMutex.Unlock()

		conn.Close()
		log.Info().
			Str("remote_addr", remoteAddr).
			Int("remaining_connections", connCount).
			Msg("WebSocket connection closed")
	}()

	// Configure connection
	conn.SetReadLimit(s.config.MaxMessageSize)
	conn.SetReadDeadline(time.Now().Add(s.config.PongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(s.config.PongWait))
		return nil
	})

	// Start ping ticker
	ticker := time.NewTicker(s.config.PingInterval)
	defer ticker.Stop()

	// Ping sender goroutine
	go func() {
		for {
			select {
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(s.config.WriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-s.shutdownChan:
				return
			}
		}
	}()

	// Read messages
	for {
		select {
		case <-s.shutdownChan:
			return
		default:
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				// Only log unexpected close errors (exclude normal close, going away, no status)
				if websocket.IsUnexpectedCloseError(err,
					websocket.CloseGoingAway,
					websocket.CloseAbnormalClosure,
					websocket.CloseNormalClosure,
					websocket.CloseNoStatusReceived) {
					log.Error().Err(err).Str("remote_addr", remoteAddr).Msg("WebSocket error")
				}

				return
			}

			if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
				s.processMessage(conn, message, remoteAddr)
			}
		}
	}
}

// processMessage processes a received log message
func (s *WebSocketServer) processMessage(conn *websocket.Conn, message []byte, source string) {
	// Prepare response
	response := &LogResponse{
		Timestamp: time.Now(),
	}

	// Send response to client at the end
	defer func() {
		if err := s.sendResponse(conn, response); err != nil {
			log.Warn().
				Err(err).
				Str("source", source).
				Msg("Failed to send response to client")
		}
	}()

	// First, try to parse as a generic map to detect the log type
	var rawLog map[string]any
	if err := json.Unmarshal(message, &rawLog); err != nil {
		response.Status = "error"
		response.Message = "Failed to parse log message: " + err.Error()

		log.Warn().
			Err(err).
			Str("source", source).
			Str("raw_message", string(message)).
			Msg("Failed to parse log message")

		return
	}

	var processedLog *processor.LogEntry
	var logType string

	// Detect if this is a control system log by checking for control system-specific fields
	if s.isControlSystemLog(rawLog) {
		var csEntry processor.ControlSystemLogEntry
		if err := json.Unmarshal(message, &csEntry); err != nil {
			response.Status = "error"
			response.Message = "Failed to parse control system log: " + err.Error()

			log.Warn().
				Err(err).
				Str("source", source).
				Msg("Failed to parse control system log")
			return
		}
		processedLog = s.processor.ProcessControlSystem(&csEntry, source)
		logType = "control_system"
		response.ID = csEntry.ID
		response.Type = logType
	} else {
		// Process as generic log
		processedLog = s.processor.Process(rawLog, source)
		logType = "generic"
		response.Type = logType
		// Try to extract ID from generic log if present
		if id, ok := rawLog["id"].(string); ok && id != "" {
			response.ID = id
		}
	}

	// Extract common fields for logging
	logEvent := log.Info().
		Str("source", source).
		Str("type", logType)

	// Add level if present
	if level, ok := rawLog["level"].(string); ok {
		logEvent = logEvent.Str("log_level", level)
	}

	// Add app/service if present
	if app, ok := rawLog["app"].(string); ok && app != "" {
		logEvent = logEvent.Str("app", app)
	} else if service, ok := rawLog["service"].(string); ok && service != "" {
		logEvent = logEvent.Str("app", service)
	}

	// Add message preview (truncate if too long)
	if msg, ok := rawLog["message"].(string); ok {
		if len(msg) > 100 {
			logEvent = logEvent.Str("log_message", msg[:100]+"...")
		} else {
			logEvent = logEvent.Str("log_message", msg)
		}
	}

	logEvent.Msg("Log message received and processed")

	// Send to Loki
	if err := s.lokiClient.Push(processedLog); err != nil {
		response.Status = "error"
		response.Message = "Failed to push log to Loki: " + err.Error()

		log.Error().
			Err(err).
			Str("source", source).
			Msg("Failed to push log to Loki")
		return
	}

	// Success
	response.Status = "success"
}

// sendResponse sends a JSON response back to the client via WebSocket
func (s *WebSocketServer) sendResponse(conn *websocket.Conn, response *LogResponse) error {
	conn.SetWriteDeadline(time.Now().Add(s.config.WriteWait))
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// isControlSystemLog detects if a log entry is a control system log
func (s *WebSocketServer) isControlSystemLog(logData map[string]any) bool {
	// Check for control system-specific fields
	_, hasClientID := logData["clientId"]
	_, hasRoomName := logData["roomName"]
	_, hasSystemType := logData["systemType"]

	// If it has any control system-specific fields, treat it as a control system log
	return hasClientID || hasRoomName || hasSystemType
}

// Shutdown gracefully shuts down the WebSocket server
func (s *WebSocketServer) Shutdown(ctx context.Context) {
	log.Info().Msg("Initiating WebSocket server shutdown")

	// Signal all goroutines to stop
	close(s.shutdownChan)

	// Close all active connections
	s.connMutex.Lock()
	for conn := range s.connections {
		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "Server shutting down"),
			time.Now().Add(s.config.WriteWait),
		)

		conn.Close()
	}

	s.connMutex.Unlock()

	// Wait for all connection handlers to finish
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().Msg("All WebSocket connections closed gracefully")
	case <-ctx.Done():
		log.Warn().Msg("Shutdown timeout exceeded, forcing close")
	}
}

// HandleReady returns readiness status
func (s *WebSocketServer) HandleReady(w http.ResponseWriter, r *http.Request) {
	s.connMutex.RLock()
	connCount := len(s.connections)
	s.connMutex.RUnlock()

	response := map[string]any{
		"ready":           true,
		"connections":     connCount,
		"max_connections": s.config.MaxConnections,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
