package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
)

// TestLedgerReadyReportsWaking proves a ClickHouse that accepts the TCP
// connection but never answers the ping is waking rather than unreachable.
func TestLedgerReadyReportsWaking(t *testing.T) {
	setProbeResponseBudget(t, 250*time.Millisecond)

	silent := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(silent.Close)

	status := ledgerReadyStatus(t, readyServerURL(t, standInConfig(t, silent.URL)))
	if status != "waking" {
		t.Fatalf("ledger readiness status = %q, want waking", status)
	}
}

// TestLedgerReadyReportsUnreachableBeforeBudgetExpires proves a refused dial
// fails the route long before the response budget runs out.
func TestLedgerReadyReportsUnreachableBeforeBudgetExpires(t *testing.T) {
	budget := 30 * time.Second
	setProbeResponseBudget(t, budget)

	cfg := &config.Config{
		ClickHouseHost:     "127.0.0.1",
		ClickHousePort:     closedPort(t),
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
	}
	started := time.Now()
	status := ledgerReadyStatus(t, readyServerURL(t, cfg))
	elapsed := time.Since(started)
	if status != "unreachable" {
		t.Fatalf("ledger readiness status = %q, want unreachable", status)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("ledger readiness took %s with a %s budget, want a fast dial failure", elapsed, budget)
	}
}

// TestLedgerReadyAnswersStalledPingBodyWithoutWaiting proves the probe decides
// on the status line. A 200 status line followed by a stalled body must not
// hold the route for the response budget.
func TestLedgerReadyAnswersStalledPingBodyWithoutWaiting(t *testing.T) {
	const budget = 2 * time.Second
	setProbeResponseBudget(t, budget)

	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(stalled.Close)

	started := time.Now()
	code, status := ledgerReadyResult(t, readyServerURL(t, standInConfig(t, stalled.URL)))
	elapsed := time.Since(started)
	if code != http.StatusOK {
		t.Fatalf("ledger readiness = %d, want 200", code)
	}
	if status != "ready" {
		t.Fatalf("ledger readiness status = %q, want ready", status)
	}
	if elapsed >= budget/4 {
		t.Fatalf("ledger readiness took %s with a %s budget, want an answer from the status line", elapsed, budget)
	}
}

// TestLedgerReadyHonorsDialTimeout proves the probe bounds connection
// establishment with probeDialTimeout rather than the response budget. A one
// nanosecond dial against a live listener fails at once, so the route reports
// unreachable instead of reading the ready answer.
func TestLedgerReadyHonorsDialTimeout(t *testing.T) {
	setProbeDialTimeout(t, time.Nanosecond)
	setProbeResponseBudget(t, 5*time.Second)

	ping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			_, _ = w.Write([]byte("Ok."))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ping.Close)

	started := time.Now()
	status := ledgerReadyStatus(t, readyServerURL(t, standInConfig(t, ping.URL)))
	elapsed := time.Since(started)
	if status != "unreachable" {
		t.Fatalf("ledger readiness status = %q, want unreachable", status)
	}
	if elapsed > time.Second {
		t.Fatalf("ledger readiness took %s with a 1ns dial timeout, want a fast dial failure", elapsed)
	}
}

// TestLedgerReadySurvivesSlowResolution proves resolution runs under its own
// budget rather than the dial timeout. The stand-in resolver takes longer than
// probeDialTimeout to answer, so a probe that folded resolution into the dial
// would report a healthy ledger unreachable.
func TestLedgerReadySurvivesSlowResolution(t *testing.T) {
	setProbeDialTimeout(t, 100*time.Millisecond)
	setProbeResolveTimeout(t, 5*time.Second)
	setProbeLookup(t, func(ctx context.Context, _ string) ([]string, error) {
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []string{"127.0.0.1"}, nil
	})

	ping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			_, _ = w.Write([]byte("Ok."))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ping.Close)

	code, status := ledgerReadyResult(t, readyServerURL(t, standInConfig(t, ping.URL)))
	if code != http.StatusOK || status != "ready" {
		t.Fatalf("ledger readiness = %d %q, want 200 ready after a slow resolution", code, status)
	}
}

// TestLedgerReadyReportsUnreachableOnResolutionFailure proves a resolver that
// never answers reports unreachable within the resolution budget rather than
// holding the route open.
func TestLedgerReadyReportsUnreachableOnResolutionFailure(t *testing.T) {
	setProbeResolveTimeout(t, 150*time.Millisecond)
	setProbeLookup(t, func(ctx context.Context, _ string) ([]string, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	cfg := &config.Config{
		ClickHouseHost:     "ledger.example",
		ClickHousePort:     8123,
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
	}
	started := time.Now()
	status := ledgerReadyStatus(t, readyServerURL(t, cfg))
	elapsed := time.Since(started)
	if status != "unreachable" {
		t.Fatalf("ledger readiness status = %q, want unreachable", status)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("ledger readiness took %s with a 150ms resolution budget, want a bounded failure", elapsed)
	}
}

// setProbeResponseBudget sets the probe budget for one test. The cleanup runs
// after every server the test starts has closed, so no handler reads the
// budget while the test restores it.
func setProbeResponseBudget(t *testing.T, budget time.Duration) {
	t.Helper()
	previous := probeResponseBudget
	t.Cleanup(func() { probeResponseBudget = previous })
	probeResponseBudget = budget
}

// setProbeDialTimeout sets the dial timeout for one test. The cleanup runs
// after every server the test starts has closed, so no handler reads the
// timeout while the test restores it.
func setProbeDialTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	previous := probeDialTimeout
	t.Cleanup(func() { probeDialTimeout = previous })
	probeDialTimeout = timeout
}

// readyServerURL mounts the API handler over cfg and returns its URL.
func readyServerURL(t *testing.T, cfg *config.Config) string {
	t.Helper()
	server, err := NewServer(cfg, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

// standInConfig points a config at a stand-in ClickHouse URL.
func standInConfig(t *testing.T, rawURL string) *config.Config {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse stand-in URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse stand-in port: %v", err)
	}
	return &config.Config{
		ClickHouseHost:     parsed.Hostname(),
		ClickHousePort:     port,
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
	}
}

// ledgerReadyResult requests the readiness route and returns the HTTP status
// code and the JSON status field.
func ledgerReadyResult(t *testing.T, serverURL string) (int, string) {
	t.Helper()
	response, err := http.Get(serverURL + "/api/ledger/ready")
	if err != nil {
		t.Fatalf("get ledger readiness: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read ledger readiness: %v", err)
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode ledger readiness %q: %v", body, err)
	}
	return response.StatusCode, payload.Status
}

// ledgerReadyStatus requests the readiness route and returns its status field.
// It requires the 503 that every non-ready outcome reports.
func ledgerReadyStatus(t *testing.T, serverURL string) string {
	t.Helper()
	code, status := ledgerReadyResult(t, serverURL)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("ledger readiness = %d, want 503", code)
	}
	return status
}

// setProbeResolveTimeout sets the resolution budget for one test.
func setProbeResolveTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	previous := probeResolveTimeout
	t.Cleanup(func() { probeResolveTimeout = previous })
	probeResolveTimeout = timeout
}

// setProbeLookup substitutes the probe resolver for one test.
func setProbeLookup(t *testing.T, lookup func(context.Context, string) ([]string, error)) {
	t.Helper()
	previous := probeLookup
	t.Cleanup(func() { probeLookup = previous })
	probeLookup = lookup
}

// closedPort returns a port that nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a closed port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved port: %v", err)
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split closed address: %v", err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("parse closed port: %v", err)
	}
	return number
}
