package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
)

const defaultBatchSize = 100
const reconciliationInterval = time.Second

// Client persists ClickHouse inserts through a durable local queue.
type Client struct {
	endpoint *url.URL
	database string
	user     string
	password string
	http     *http.Client
	queue    *Queue

	reconcilerCancel context.CancelFunc
	reconcilerDone   chan struct{}
	closeOnce        sync.Once
}

// Option changes a Client dependency. It supports local HTTP test servers and callers
// that need a transport with explicit timeouts.
type Option func(*Client) error

// WithHTTPClient supplies the HTTP client used for ClickHouse requests.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) error {
		if httpClient == nil {
			return errors.New("ledger HTTP client is nil")
		}
		client.http = httpClient
		return nil
	}
}

// WithEndpoint replaces the configured ClickHouse endpoint. It is useful for local
// ClickHouse instances and black-box transport tests.
func WithEndpoint(endpoint string) Option {
	return func(client *Client) error {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid ClickHouse endpoint %q", endpoint)
		}
		client.endpoint = parsed
		return nil
	}
}

// New creates a client from application configuration and opens its durable queue.
// queueDir must live on persistent local storage, not a temporary filesystem.
func New(cfg *config.Config, queueDir string, options ...Option) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("ledger config is nil")
	}
	if strings.TrimSpace(cfg.ClickHouseHost) == "" {
		return nil, errors.New("ledger ClickHouse host is empty")
	}
	if strings.TrimSpace(cfg.ClickHouseUser) == "" {
		return nil, errors.New("ledger ClickHouse user is empty")
	}
	if strings.TrimSpace(cfg.ClickHousePassword) == "" {
		return nil, errors.New("ledger ClickHouse password is empty")
	}
	if strings.TrimSpace(cfg.ClickHouseDatabase) == "" {
		return nil, errors.New("ledger ClickHouse database is empty")
	}
	scheme := "http"
	if cfg.ClickHouseSecure {
		scheme = "https"
	}
	endpoint := &url.URL{Scheme: scheme, Host: fmt.Sprintf("%s:%d", cfg.ClickHouseHost, cfg.ClickHousePort)}
	queue, err := OpenQueue(queueDir, defaultBatchSize)
	if err != nil {
		return nil, err
	}
	client := &Client{
		endpoint: endpoint,
		database: cfg.ClickHouseDatabase,
		user:     cfg.ClickHouseUser,
		password: cfg.ClickHousePassword,
		http:     &http.Client{Timeout: 30 * time.Second},
		queue:    queue,
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("ledger client option is nil")
		}
		if err := option(client); err != nil {
			return nil, err
		}
	}
	client.startReconciler()
	return client, nil
}

// startReconciler retries durable entries without requiring a later caller action.
// The queue serializes retries with foreground flushes and removes entries only after
// ClickHouse acknowledges them.
func (c *Client) startReconciler() {
	ctx, cancel := context.WithCancel(context.Background())
	c.reconcilerCancel = cancel
	c.reconcilerDone = make(chan struct{})
	go func() {
		defer close(c.reconcilerDone)
		ticker := time.NewTicker(reconciliationInterval)
		defer ticker.Stop()
		for {
			_ = c.Flush(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Close stops and joins the automatic reconciler. It is safe to call more than once.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.reconcilerCancel != nil {
			c.reconcilerCancel()
		}
		if c.reconcilerDone != nil {
			<-c.reconcilerDone
		}
		if c.http != nil {
			c.http.CloseIdleConnections()
		}
	})
	return nil
}

// EnqueueJSON records one row locally, then attempts to flush all retained rows.
// A network failure returns ErrPending while leaving the row durable for reconciliation.
func (c *Client) EnqueueJSON(ctx context.Context, insert string, row any) error {
	if err := validateInsert(insert); err != nil {
		return err
	}
	body, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("encode ledger row: %w", err)
	}
	body = append(body, '\n')
	if err := c.queue.Enqueue(Entry{Query: insert, Body: body}); err != nil {
		return err
	}
	return c.Flush(ctx)
}

// Flush reconciles all locally durable rows with ClickHouse.
func (c *Client) Flush(ctx context.Context) error {
	if c == nil || c.queue == nil {
		return errors.New("ledger client is nil")
	}
	return c.queue.Flush(ctx, c.send)
}

// Pending reports the number of rows retained locally for reconciliation.
func (c *Client) Pending() (int, error) {
	if c == nil || c.queue == nil {
		return 0, errors.New("ledger client is nil")
	}
	return c.queue.Len()
}

func (c *Client) send(ctx context.Context, batch []Entry) error {
	if len(batch) == 0 {
		return nil
	}
	var payload bytes.Buffer
	for _, entry := range batch {
		payload.Write(entry.Body)
		if len(entry.Body) == 0 || entry.Body[len(entry.Body)-1] != '\n' {
			payload.WriteByte('\n')
		}
	}
	requestURL := *c.endpoint
	query := requestURL.Query()
	query.Set("database", c.database)
	query.Set("query", batch[0].Query)
	requestURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), &payload)
	if err != nil {
		return fmt.Errorf("create ClickHouse request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.SetBasicAuth(c.user, c.password)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send ClickHouse request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("ClickHouse returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
}

func validateInsert(query string) error {
	trimmed := strings.TrimSpace(query)
	if !strings.HasPrefix(strings.ToUpper(trimmed), "INSERT ") {
		return errors.New("ledger query must be an INSERT statement")
	}
	if strings.Contains(trimmed, ";") {
		return errors.New("ledger query must contain one statement")
	}
	if !strings.Contains(strings.ToUpper(trimmed), "FORMAT JSONEACHROW") {
		return errors.New("ledger query must use FORMAT JSONEachRow")
	}
	return nil
}
