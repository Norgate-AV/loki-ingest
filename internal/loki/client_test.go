package loki

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Norgate-AV/loki-ingest/internal/config"
	"github.com/Norgate-AV/loki-ingest/internal/processor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestLokiConfig(lokiURL string) *config.Config {
	return &config.Config{
		ServerPort:         "8080",
		Environment:        "test",
		LogLevel:           "info",
		MaxConnections:     10,
		MessageChannelSize: 100,
		LokiURL:            lokiURL,
		LokiBatchSize:      5,
		LokiBatchWait:      100 * time.Millisecond,
		LokiTimeout:        5 * time.Second,
		LokiRetryAttempts:  2,
		LokiRetryWait:      50 * time.Millisecond,
		LokiMaxBackoff:     500 * time.Millisecond,
		LokiDefaultLabels: map[string]string{
			"job": "test-job",
		},
	}
}

func TestNewClient(t *testing.T) {
	cfg := createTestLokiConfig("http://localhost:3100")
	client := NewClient(cfg)
	defer client.Close()

	assert.NotNil(t, client)
	assert.NotNil(t, client.config)
	assert.NotNil(t, client.httpClient)
	assert.NotNil(t, client.batchChan)
	assert.NotNil(t, client.stopChan)
}

func TestPush(t *testing.T) {
	cfg := createTestLokiConfig("http://localhost:3100")
	client := NewClient(cfg)
	defer client.Close()

	entry := &processor.LogEntry{
		Labels: map[string]string{
			"level": "info",
			"app":   "test",
		},
		Timestamp: time.Now(),
		Line:      "Test log message",
	}

	err := client.Push(entry)
	assert.NoError(t, err, "Should be able to push entry")
}

func TestLabelsToKey(t *testing.T) {
	cfg := createTestLokiConfig("http://localhost:3100")
	client := NewClient(cfg)
	defer client.Close()

	tests := []struct {
		name   string
		labels map[string]string
	}{
		{
			name: "single label",
			labels: map[string]string{
				"level": "info",
			},
		},
		{
			name: "multiple labels",
			labels: map[string]string{
				"level": "error",
				"app":   "test",
				"env":   "prod",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key1 := client.labelsToKey(tt.labels)
			key2 := client.labelsToKey(tt.labels)

			assert.NotEmpty(t, key1)
			assert.Equal(t, key1, key2, "Same labels should produce same key")
		})
	}
}

func TestLabelMerging(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var pushReq LokiPushRequest
		err := json.Unmarshal(body, &pushReq)
		require.NoError(t, err)

		// Verify default labels are merged
		assert.Greater(t, len(pushReq.Streams), 0)
		for _, stream := range pushReq.Streams {
			assert.Equal(t, "test-job", stream.Stream["job"], "Should have default job label")
			assert.NotEmpty(t, stream.Stream["level"], "Should have entry-specific label")
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	client := NewClient(cfg)
	defer client.Close()

	entry := &processor.LogEntry{
		Labels: map[string]string{
			"level": "info",
		},
		Timestamp: time.Now(),
		Line:      "Test message",
	}

	client.Push(entry)
	time.Sleep(200 * time.Millisecond) // Wait for batch to send
}

func TestBatchingBySizeLimit(t *testing.T) {
	receivedBatches := make([]LokiPushRequest, 0)
	var mu sync.Mutex

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var pushReq LokiPushRequest
		json.Unmarshal(body, &pushReq)

		mu.Lock()
		receivedBatches = append(receivedBatches, pushReq)
		mu.Unlock()

		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	cfg.LokiBatchSize = 3
	cfg.LokiBatchWait = 5 * time.Second // Long wait so size triggers batch
	client := NewClient(cfg)
	defer client.Close()

	// Push exactly batch size entries
	for i := 0; i < 3; i++ {
		entry := &processor.LogEntry{
			Labels: map[string]string{
				"level": "info",
			},
			Timestamp: time.Now(),
			Line:      "Test message",
		}
		client.Push(entry)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, len(receivedBatches), "Should send one batch when size limit reached")
}

func TestBatchingByTimeLimit(t *testing.T) {
	receivedBatches := make([]LokiPushRequest, 0)
	var mu sync.Mutex

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var pushReq LokiPushRequest
		json.Unmarshal(body, &pushReq)

		mu.Lock()
		receivedBatches = append(receivedBatches, pushReq)
		mu.Unlock()

		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	cfg.LokiBatchSize = 100 // Large size so time triggers batch
	cfg.LokiBatchWait = 100 * time.Millisecond
	client := NewClient(cfg)
	defer client.Close()

	// Push 2 entries (less than batch size)
	for i := 0; i < 2; i++ {
		entry := &processor.LogEntry{
			Labels: map[string]string{
				"level": "info",
			},
			Timestamp: time.Now(),
			Line:      "Test message",
		}
		client.Push(entry)
	}

	time.Sleep(250 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, len(receivedBatches), "Should send batch after time limit")

	if len(receivedBatches) > 0 {
		totalEntries := 0
		for _, stream := range receivedBatches[0].Streams {
			totalEntries += len(stream.Values)
		}
		assert.Equal(t, 2, totalEntries, "Should have 2 entries in batch")
	}
}

func TestStreamGroupingByLabels(t *testing.T) {
	var receivedRequest LokiPushRequest
	var mu sync.Mutex

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		json.Unmarshal(body, &receivedRequest)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	cfg.LokiBatchSize = 10
	cfg.LokiBatchWait = 100 * time.Millisecond
	client := NewClient(cfg)
	defer client.Close()

	// Push entries with different labels
	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: time.Now(),
		Line:      "Info message 1",
	})

	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "error"},
		Timestamp: time.Now(),
		Line:      "Error message 1",
	})

	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: time.Now(),
		Line:      "Info message 2",
	})

	time.Sleep(250 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have 2 streams (info and error)
	assert.GreaterOrEqual(t, len(receivedRequest.Streams), 2, "Should group by label sets")
}

func TestRetryOnFailure(t *testing.T) {
	attempts := atomic.Int32{}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	cfg.LokiBatchSize = 1
	cfg.LokiRetryAttempts = 3
	cfg.LokiRetryWait = 50 * time.Millisecond
	client := NewClient(cfg)
	defer client.Close()

	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: time.Now(),
		Line:      "Test message",
	})

	time.Sleep(500 * time.Millisecond)

	finalAttempts := attempts.Load()
	assert.Equal(t, int32(3), finalAttempts, "Should retry on failure")
}

func TestRequestHeaders(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/loki/api/v1/push")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	cfg.LokiBatchSize = 1
	client := NewClient(cfg)
	defer client.Close()

	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: time.Now(),
		Line:      "Test",
	})

	time.Sleep(200 * time.Millisecond)
}

func TestBasicAuth(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok, "Should have basic auth")
		assert.Equal(t, "testuser", user)
		assert.Equal(t, "testpass", pass)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	cfg.LokiBatchSize = 1
	cfg.LokiBasicAuthUser = "testuser"
	cfg.LokiBasicAuthPass = "testpass"
	client := NewClient(cfg)
	defer client.Close()

	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: time.Now(),
		Line:      "Test",
	})

	time.Sleep(200 * time.Millisecond)
}

func TestTenantID(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-tenant", r.Header.Get("X-Scope-OrgID"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	cfg.LokiBatchSize = 1
	cfg.LokiTenantID = "test-tenant"
	client := NewClient(cfg)
	defer client.Close()

	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: time.Now(),
		Line:      "Test",
	})

	time.Sleep(200 * time.Millisecond)
}

func TestTimestampFormat(t *testing.T) {
	var receivedRequest LokiPushRequest
	var mu sync.Mutex

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		json.Unmarshal(body, &receivedRequest)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	cfg.LokiBatchSize = 1
	client := NewClient(cfg)
	defer client.Close()

	testTime := time.Date(2026, 1, 26, 12, 0, 0, 0, time.UTC)
	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: testTime,
		Line:      "Test",
	})

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Greater(t, len(receivedRequest.Streams), 0)
	require.Greater(t, len(receivedRequest.Streams[0].Values), 0)

	// Verify timestamp is in nanoseconds
	timestampStr := receivedRequest.Streams[0].Values[0][0]
	assert.NotEmpty(t, timestampStr)
	assert.Len(t, timestampStr, 19, "Timestamp should be in nanoseconds (19 digits)")
}

func TestGracefulShutdown(t *testing.T) {
	receivedCount := atomic.Int32{}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var pushReq LokiPushRequest
		json.Unmarshal(body, &pushReq)

		for _, stream := range pushReq.Streams {
			receivedCount.Add(int32(len(stream.Values)))
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := createTestLokiConfig(mockServer.URL)
	cfg.LokiBatchSize = 100 // Large batch to prevent auto-send
	cfg.LokiBatchWait = 10 * time.Second
	client := NewClient(cfg)

	// Push some entries
	for i := 0; i < 5; i++ {
		client.Push(&processor.LogEntry{
			Labels:    map[string]string{"level": "info"},
			Timestamp: time.Now(),
			Line:      "Test message",
		})
	}

	// Close immediately - should drain and send
	client.Close()

	// All entries should have been sent
	assert.Equal(t, int32(5), receivedCount.Load(), "All entries should be sent on shutdown")
}

func TestPushChannelFull(t *testing.T) {
	cfg := createTestLokiConfig("http://localhost:3100")
	cfg.MessageChannelSize = 2
	client := NewClient(cfg)
	defer client.Close()

	// Fill the channel
	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: time.Now(),
		Line:      "Message 1",
	})
	client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: time.Now(),
		Line:      "Message 2",
	})

	// This should timeout with the 1 second timeout
	start := time.Now()
	err := client.Push(&processor.LogEntry{
		Labels:    map[string]string{"level": "info"},
		Timestamp: time.Now(),
		Line:      "Message 3",
	})
	duration := time.Since(start)

	// Should either succeed quickly or timeout after ~1 second
	if err != nil {
		assert.Contains(t, err.Error(), "full")
		assert.GreaterOrEqual(t, duration, 900*time.Millisecond, "Should wait for timeout")
	}
}
