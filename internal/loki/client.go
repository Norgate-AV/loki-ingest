package loki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Norgate-AV/loki-ingest/internal/config"
	"github.com/Norgate-AV/loki-ingest/internal/processor"

	"github.com/rs/zerolog/log"
)

// Client handles communication with Loki
type Client struct {
	config     *config.Config
	httpClient *http.Client
	batchChan  chan *processor.LogEntry
	stopChan   chan struct{}
	wg         sync.WaitGroup
}

// LokiPushRequest represents the Loki push API request format
type LokiPushRequest struct {
	Streams []Stream `json:"streams"`
}

// Stream represents a stream in the Loki push request
type Stream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// NewClient creates a new Loki client
func NewClient(cfg *config.Config) *Client {
	client := &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.LokiTimeout,
		},
		batchChan: make(chan *processor.LogEntry, cfg.MessageChannelSize),
		stopChan:  make(chan struct{}),
	}

	log.Info().
		Str("loki_url", cfg.LokiURL).
		Int("batch_size", cfg.LokiBatchSize).
		Dur("batch_wait", cfg.LokiBatchWait).
		Int("channel_size", cfg.MessageChannelSize).
		Msg("Loki client initialized")

	// Start batch processor
	client.wg.Add(1)
	go client.batchProcessor()

	return client
}

// Push queues a log entry for sending to Loki
func (c *Client) Push(entry *processor.LogEntry) error {
	select {
	case c.batchChan <- entry:
		return nil
	case <-c.stopChan:
		return fmt.Errorf("client is shutting down")
	default:
		// Channel is full, log warning and try with timeout
		log.Warn().Msg("Batch channel full, attempting to queue with timeout")
		select {
		case c.batchChan <- entry:
			log.Info().Msg("Log entry queued after wait")
			return nil
		case <-time.After(1 * time.Second):
			log.Error().Msg("Batch channel full, log entry dropped")
			return fmt.Errorf("batch channel full, entry dropped")
		}
	}
}

// batchProcessor collects log entries and sends them in batches
func (c *Client) batchProcessor() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.LokiBatchWait)
	defer ticker.Stop()

	batch := make([]*processor.LogEntry, 0, c.config.LokiBatchSize)

	for {
		select {
		case entry := <-c.batchChan:
			batch = append(batch, entry)
			if len(batch) >= c.config.LokiBatchSize {
				c.sendBatch(batch)
				batch = make([]*processor.LogEntry, 0, c.config.LokiBatchSize)
			}

		case <-ticker.C:
			if len(batch) > 0 {
				c.sendBatch(batch)
				batch = make([]*processor.LogEntry, 0, c.config.LokiBatchSize)
			}

		case <-c.stopChan:
			// Send remaining batch before shutting down
			if len(batch) > 0 {
				c.sendBatch(batch)
			}

			return
		}
	}
}

// sendBatch sends a batch of log entries to Loki
func (c *Client) sendBatch(entries []*processor.LogEntry) {
	if len(entries) == 0 {
		return
	}

	// Group entries by label set
	streams := make(map[string]*Stream)

	for _, entry := range entries {
		// Merge default labels with entry labels
		labels := make(map[string]string)
		maps.Copy(labels, c.config.LokiDefaultLabels)
		maps.Copy(labels, entry.Labels)

		// Create a key from sorted labels
		labelKey := c.labelsToKey(labels)

		// Get or create stream
		stream, exists := streams[labelKey]
		if !exists {
			stream = &Stream{
				Stream: labels,
				Values: make([][]string, 0),
			}
			streams[labelKey] = stream
		}

		// Add log entry to stream
		// Loki expects [timestamp_ns, log_line]
		timestampNs := strconv.FormatInt(entry.Timestamp.UnixNano(), 10)
		stream.Values = append(stream.Values, []string{timestampNs, entry.Line})
	}

	// Convert map to slice
	streamSlice := make([]Stream, 0, len(streams))
	for _, stream := range streams {
		streamSlice = append(streamSlice, *stream)
	}

	// Create push request
	pushRequest := LokiPushRequest{
		Streams: streamSlice,
	}

	// Send with retry
	if err := c.sendWithRetry(pushRequest); err != nil {
		log.Error().
			Err(err).
			Int("batch_size", len(entries)).
			Msg("Failed to send batch to Loki after retries")
	} else {
		log.Info().
			Int("batch_size", len(entries)).
			Int("streams", len(streamSlice)).
			Msg("Successfully sent batch to Loki")
	}
}

// sendWithRetry sends a request to Loki with exponential backoff retry
func (c *Client) sendWithRetry(pushRequest LokiPushRequest) error {
	var lastErr error
	backoff := c.config.LokiRetryWait

	for attempt := 0; attempt <= c.config.LokiRetryAttempts; attempt++ {
		if attempt > 0 {
			log.Warn().
				Int("attempt", attempt).
				Dur("backoff", backoff).
				Msg("Retrying Loki push")
			time.Sleep(backoff)

			// Exponential backoff
			backoff *= 2
			if backoff > c.config.LokiMaxBackoff {
				backoff = c.config.LokiMaxBackoff
			}
		}

		err := c.send(pushRequest)
		if err == nil {
			return nil
		}

		lastErr = err
	}

	return fmt.Errorf("failed after %d attempts: %w", c.config.LokiRetryAttempts, lastErr)
}

// send sends a push request to Loki
func (c *Client) send(pushRequest LokiPushRequest) error {
	// Marshal request body
	body, err := json.Marshal(pushRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	url := c.config.LokiURL + "/loki/api/v1/push"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Add tenant ID if configured
	if c.config.LokiTenantID != "" {
		req.Header.Set("X-Scope-OrgID", c.config.LokiTenantID)
	}

	// Add basic auth if configured
	if c.config.LokiBasicAuthUser != "" {
		req.SetBasicAuth(c.config.LokiBasicAuthUser, c.config.LokiBasicAuthPass)
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("loki returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// labelsToKey creates a consistent key from a label set for grouping
func (c *Client) labelsToKey(labels map[string]string) string {
	// Create a consistent key by concatenating sorted label pairs
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(labels[k])
		b.WriteString(";")
	}

	return b.String()
}

// Close gracefully shuts down the Loki client
func (c *Client) Close() {
	log.Info().Msg("Closing Loki client")
	close(c.stopChan)
	c.wg.Wait()

	// Drain remaining entries
	close(c.batchChan)
	remaining := make([]*processor.LogEntry, 0)
	for entry := range c.batchChan {
		remaining = append(remaining, entry)
	}

	if len(remaining) > 0 {
		log.Info().Int("count", len(remaining)).Msg("Sending remaining log entries to Loki")
		c.sendBatch(remaining)
	}

	log.Info().Msg("Loki client closed")
}
