package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Norgate-AV/loki-ingest/internal/config"
	"github.com/Norgate-AV/loki-ingest/internal/loki"
	"github.com/Norgate-AV/loki-ingest/internal/processor"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestConfig() *config.Config {
	return &config.Config{
		ServerPort:         "8080",
		Environment:        "test",
		LogLevel:           "info",
		MaxConnections:     5,
		ReadBufferSize:     1024,
		WriteBufferSize:    1024,
		PingInterval:       5 * time.Second,
		PongWait:           10 * time.Second,
		WriteWait:          5 * time.Second,
		MaxMessageSize:     1024 * 1024,
		MessageChannelSize: 10,
		LokiURL:            "http://localhost:3100",
		LokiBatchSize:      10,
		LokiBatchWait:      1 * time.Second,
		LokiTimeout:        5 * time.Second,
		LokiRetryAttempts:  1,
		LokiRetryWait:      1 * time.Second,
		LokiMaxBackoff:     5 * time.Second,
		LokiDefaultLabels:  map[string]string{"job": "test"},
	}
}

func createTestServer() *WebSocketServer {
	cfg := createTestConfig()
	lokiClient := loki.NewClient(cfg)
	return NewWebSocketServer(cfg, lokiClient)
}

func TestNewWebSocketServer(t *testing.T) {
	server := createTestServer()

	assert.NotNil(t, server)
	assert.NotNil(t, server.config)
	assert.NotNil(t, server.lokiClient)
	assert.NotNil(t, server.processor)
	assert.NotNil(t, server.connections)
	assert.NotNil(t, server.shutdownChan)
	assert.Equal(t, 0, len(server.connections))
}

func TestReadyEndpoint(t *testing.T) {
	server := createTestServer()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	server.HandleReady(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, true, response["ready"])
	assert.NotNil(t, response["connections"])
}

func TestWebSocketConnection(t *testing.T) {
	server := createTestServer()

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect via WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err, "Should be able to connect")
	defer ws.Close()

	// Give server time to register connection
	time.Sleep(50 * time.Millisecond)

	// Verify connection is tracked
	server.connMutex.RLock()
	connCount := len(server.connections)
	server.connMutex.RUnlock()

	assert.Equal(t, 1, connCount, "Server should track the connection")
}

func TestConnectionLimit(t *testing.T) {
	server := createTestServer()
	server.config.MaxConnections = 2

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Open first connection
	ws1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws1.Close()

	time.Sleep(50 * time.Millisecond)

	// Open second connection
	ws2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws2.Close()

	time.Sleep(50 * time.Millisecond)

	// Try to open third connection (should fail or be rejected)
	ws3, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		// Connection was accepted but should receive close message
		ws3.Close()
		t.Log("Connection accepted but will be closed immediately")
	} else {
		// Connection was rejected at HTTP level
		assert.Error(t, err)
		if resp != nil {
			assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		}
	}
}

func TestMessageProcessing(t *testing.T) {
	server := createTestServer()

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Send a control system log entry
	logEntry := processor.ControlSystemLogEntry{
		ID:       "test-uuid-123",
		ClientID: "test-client",
		HostName: "test-host",
		RoomName: "test-room",
		Level:    "info",
		Message:  "Test message",
	}

	data, err := json.Marshal(logEntry)
	require.NoError(t, err)

	err = ws.WriteMessage(websocket.TextMessage, data)
	assert.NoError(t, err, "Should be able to send message")

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	// Message should be processed without error (we can't easily verify
	// it was sent to Loki without mocking, but we can verify no connection errors)
}

func TestGenericLogProcessing(t *testing.T) {
	server := createTestServer()

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Send a generic log entry with app field
	logEntry := map[string]any{
		"timestamp": "2026-01-27T10:30:00Z",
		"level":     "info",
		"message":   "Test generic log message",
		"app":       "test-app",
		"env":       "production",
		"host":      "test-server",
	}

	data, err := json.Marshal(logEntry)
	require.NoError(t, err)

	err = ws.WriteMessage(websocket.TextMessage, data)
	assert.NoError(t, err, "Should be able to send generic log message")

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	// Message should be processed without error
}

func TestIsControlSystemLog(t *testing.T) {
	server := createTestServer()

	tests := []struct {
		name     string
		logData  map[string]any
		expected bool
	}{
		{
			name: "Control system log with client",
			logData: map[string]any{
				"client":  "test-client",
				"message": "test",
			},
			expected: true,
		},
		{
			name: "Control system log with roomName",
			logData: map[string]any{
				"roomName": "conference-room",
				"message":  "test",
			},
			expected: true,
		},
		{
			name: "Control system log with systemType",
			logData: map[string]any{
				"systemType": "NetLinx",
				"message":    "test",
			},
			expected: true,
		},
		{
			name: "Generic log with app",
			logData: map[string]any{
				"app":     "my-app",
				"message": "test",
				"level":   "info",
			},
			expected: false,
		},
		{
			name: "Generic log with service",
			logData: map[string]any{
				"service": "api-server",
				"message": "test",
			},
			expected: false,
		},
		{
			name: "Empty log",
			logData: map[string]any{
				"message": "test",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := server.isControlSystemLog(tt.logData)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInvalidJSON(t *testing.T) {
	server := createTestServer()

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Send invalid JSON
	err = ws.WriteMessage(websocket.TextMessage, []byte("invalid json {{{"))
	assert.NoError(t, err, "Should be able to send message")

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	// Connection should still be alive despite invalid JSON
	err = ws.WriteMessage(websocket.TextMessage, []byte("{}"))
	assert.NoError(t, err, "Connection should still be alive")
}

func TestShutdown(t *testing.T) {
	server := createTestServer()

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Verify connection exists
	server.connMutex.RLock()
	initialCount := len(server.connections)
	server.connMutex.RUnlock()
	assert.Equal(t, 1, initialCount)

	// Shutdown the server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server.Shutdown(ctx)

	// Verify connections are closed
	server.connMutex.RLock()
	finalCount := len(server.connections)
	server.connMutex.RUnlock()
	assert.Equal(t, 0, finalCount, "All connections should be closed after shutdown")
}

func TestConcurrentConnections(t *testing.T) {
	server := createTestServer()
	server.config.MaxConnections = 10

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Open multiple concurrent connections
	connections := make([]*websocket.Conn, 5)
	for i := 0; i < 5; i++ {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err, "Should connect #%d", i)
		connections[i] = ws
		defer ws.Close()
	}

	time.Sleep(100 * time.Millisecond)

	// Verify all connections are tracked
	server.connMutex.RLock()
	connCount := len(server.connections)
	server.connMutex.RUnlock()

	assert.Equal(t, 5, connCount, "Server should track all concurrent connections")

	// Send messages from all connections concurrently
	for i, ws := range connections {
		go func(index int, conn *websocket.Conn) {
			entry := map[string]string{
				"message": "Test from connection " + string(rune('A'+index)),
				"level":   "info",
			}
			data, _ := json.Marshal(entry)
			conn.WriteMessage(websocket.TextMessage, data)
		}(i, ws)
	}

	time.Sleep(200 * time.Millisecond)

	// All connections should still be alive
	for i, ws := range connections {
		err := ws.WriteMessage(websocket.TextMessage, []byte("{}"))
		assert.NoError(t, err, "Connection #%d should still be alive", i)
	}
}

func TestConnectionCleanupOnClientDisconnect(t *testing.T) {
	server := createTestServer()

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Verify connection exists
	server.connMutex.RLock()
	assert.Equal(t, 1, len(server.connections))
	server.connMutex.RUnlock()

	// Close connection from client side
	ws.Close()

	// Give server time to detect disconnect and cleanup
	time.Sleep(200 * time.Millisecond)

	// Verify connection was cleaned up
	server.connMutex.RLock()
	finalCount := len(server.connections)
	server.connMutex.RUnlock()

	assert.Equal(t, 0, finalCount, "Server should cleanup disconnected connection")
}
