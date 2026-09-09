package fit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	"google.golang.org/genai"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/gemini"
)

// rateLimitErr is the Vertex reply that killed the 2026-09-09 run.
func rateLimitErr() error {
	return genai.APIError{Code: 429, Message: "Resource exhausted. Please try again later.", Status: "RESOURCE_EXHAUSTED"}
}

// fastRetryPolicy bounds a test run to a few milliseconds per wait.
func fastRetryPolicy(attempts int) RetryPolicy {
	return RetryPolicy{MaxAttempts: attempts, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}
}

// timeoutErr is a net.Error that always reports a timeout.
type timeoutErr struct{}

// Error reports the timeout.
func (timeoutErr) Error() string { return "i/o timeout" }

// Timeout reports that the error is a timeout.
func (timeoutErr) Timeout() bool { return true }

// Temporary reports that the error is temporary.
func (timeoutErr) Temporary() bool { return true }

// flakyCall counts calls and answers with a scripted error sequence.
type flakyCall struct {
	mu    sync.Mutex
	errs  []error
	reply string
	calls int
}

// call consumes one scripted error and otherwise answers with the reply.
func (c *flakyCall) call() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if len(c.errs) > 0 {
		err := c.errs[0]
		c.errs = c.errs[1:]
		return "", err
	}
	return c.reply, nil
}

// count returns the calls the script has seen.
func (c *flakyCall) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// billingFlakyTranslator answers with a scripted error sequence and bills each
// successful call, which is the shape of every production client.
type billingFlakyTranslator struct {
	mu    sync.Mutex
	rec   ChargeRecorder
	errs  []error
	reply string
	calls int
}

// Translate consumes one scripted error and bills a successful call.
func (t *billingFlakyTranslator) Translate(_ context.Context, req gemini.TranslateRequest) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	if len(t.errs) > 0 {
		err := t.errs[0]
		t.errs = t.errs[1:]
		return "", err
	}
	if t.rec != nil {
		t.rec.Add(cost.Charge{
			Kind:               cost.ChargeTranslate,
			TakeID:             req.SegmentID,
			PromptTokens:       10,
			CandidateTokens:    5,
			PromptUnitPrice:    150,
			CandidateUnitPrice: 600,
		})
	}
	return t.reply, nil
}

// count returns the calls the translator has seen.
func (t *billingFlakyTranslator) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// TestIsTransientUpstreamClassifiesCauses pins which upstream failures may be
// retried. A rate limit, an unavailable service, and a reset or timed out
// connection are transient. A rejected request, an authentication failure, an
// internal error, and an ended context are not.
func TestIsTransientUpstreamClassifiesCauses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit", genai.APIError{Code: 429, Message: "Resource exhausted.", Status: "RESOURCE_EXHAUSTED"}, true},
		{"unavailable", genai.APIError{Code: 503, Status: "UNAVAILABLE"}, true},
		{"rate limit pointer", &genai.APIError{Code: 429}, true},
		{"bad request", genai.APIError{Code: 400, Status: "INVALID_ARGUMENT"}, false},
		{"unauthenticated", genai.APIError{Code: 401, Status: "UNAUTHENTICATED"}, false},
		{"permission denied", genai.APIError{Code: 403, Status: "PERMISSION_DENIED"}, false},
		{"wrapped rate limit", fmt.Errorf("translate line 5 attempt 3: %w", rateLimitErr()), true},
		{"grpc exhausted", status.Error(codes.ResourceExhausted, "quota"), true},
		{"grpc unavailable", status.Error(codes.Unavailable, "down"), true},
		{"grpc invalid argument", status.Error(codes.InvalidArgument, "bad"), false},
		{"connection reset", syscall.ECONNRESET, true},
		{"wrapped connection reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, true},
		{"timeout", timeoutErr{}, true},
		{"cancelled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"internal", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTransientUpstream(tc.err); got != tc.want {
				t.Errorf("IsTransientUpstream(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

// TestRetryTransientRecoversAfterTwoRateLimits proves two rate limits followed
// by a success complete the call, and the call count names the retries.
func TestRetryTransientRecoversAfterTwoRateLimits(t *testing.T) {
	call := &flakyCall{errs: []error{rateLimitErr(), rateLimitErr()}, reply: "ചലനം"}
	got, err := retryTransientValue(context.Background(), fastRetryPolicy(3), call.call)
	if err != nil {
		t.Fatalf("retryTransientValue: %v", err)
	}
	if got != "ചലനം" {
		t.Errorf("value = %q, want the reply", got)
	}
	if call.count() != 3 {
		t.Errorf("calls = %d, want 3 (one call and two retries)", call.count())
	}
	t.Logf("recovered after %d calls", call.count())
}

// TestRetryTransientFailsFastOnRejectedRequest proves a 400 fails on the first
// call and never retries.
func TestRetryTransientFailsFastOnRejectedRequest(t *testing.T) {
	call := &flakyCall{errs: []error{genai.APIError{Code: 400, Message: "bad request", Status: "INVALID_ARGUMENT"}}}
	_, err := retryTransientValue(context.Background(), fastRetryPolicy(3), call.call)
	if err == nil {
		t.Fatal("retryTransientValue succeeded, want the rejected request")
	}
	if call.count() != 1 {
		t.Errorf("calls = %d, want 1", call.count())
	}
	if IsTransientUpstream(err) {
		t.Error("rejected request classified as transient")
	}
	t.Logf("rejected request stopped after %d call", call.count())
}

// TestRetryTransientStopsAfterBoundedAttempts proves a rate limit that never
// clears stops at the bound and finishes quickly.
func TestRetryTransientStopsAfterBoundedAttempts(t *testing.T) {
	call := &flakyCall{errs: []error{rateLimitErr(), rateLimitErr(), rateLimitErr(), rateLimitErr(), rateLimitErr()}}
	policy := RetryPolicy{MaxAttempts: 4, BaseDelay: 5 * time.Millisecond, MaxDelay: 20 * time.Millisecond}
	started := time.Now()
	_, err := retryTransientValue(context.Background(), policy, call.call)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("retryTransientValue succeeded, want the rate limit")
	}
	if !IsTransientUpstream(err) {
		t.Errorf("error = %v, want the transient rate limit", err)
	}
	if call.count() != 4 {
		t.Errorf("calls = %d, want the bounded 4", call.count())
	}
	if elapsed > time.Second {
		t.Errorf("exhausted retries took %v, want a bounded wait", elapsed)
	}
	t.Logf("exhausted %d attempts in %v", call.count(), elapsed)
}

// TestRetryBackoffIsBoundedExponential pins the backoff window per attempt. The
// delay doubles from the base, stops at the cap, and the jitter stays inside
// the second half of the window, so no wait exceeds the cap.
func TestRetryBackoffIsBoundedExponential(t *testing.T) {
	policy := RetryPolicy{BaseDelay: 100 * time.Millisecond, MaxDelay: 400 * time.Millisecond}.normalized()
	windows := []struct {
		attempt int
		lo      time.Duration
		hi      time.Duration
	}{
		{1, 50 * time.Millisecond, 100 * time.Millisecond},
		{2, 100 * time.Millisecond, 200 * time.Millisecond},
		{3, 200 * time.Millisecond, 400 * time.Millisecond},
		{4, 200 * time.Millisecond, 400 * time.Millisecond},
		{8, 200 * time.Millisecond, 400 * time.Millisecond},
	}
	for _, window := range windows {
		for i := 0; i < 200; i++ {
			got := policy.backoff(window.attempt)
			if got < window.lo || got >= window.hi {
				t.Fatalf("backoff(%d) = %v, want [%v, %v)", window.attempt, got, window.lo, window.hi)
			}
		}
	}
}

// TestRetryTransientStopsWhenContextEnds proves a cancelled run stops the
// backoff wait and issues no further call.
func TestRetryTransientStopsWhenContextEnds(t *testing.T) {
	call := &flakyCall{errs: []error{rateLimitErr(), rateLimitErr()}, reply: "x"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	policy := RetryPolicy{MaxAttempts: 5, BaseDelay: 50 * time.Millisecond, MaxDelay: 50 * time.Millisecond}
	_, err := retryTransientValue(ctx, policy, call.call)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if call.count() != 1 {
		t.Errorf("calls = %d, want 1 before the context ended", call.count())
	}
}

// TestRetryTransientSkipsAnEndedContext proves a call never starts once the
// context ended.
func TestRetryTransientSkipsAnEndedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	call := &flakyCall{reply: "x"}
	_, err := retryTransientValue(ctx, DefaultRetryPolicy(), call.call)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if call.count() != 0 {
		t.Errorf("calls = %d, want 0", call.count())
	}
}

// TestRetryingTranslatorBillsOneChargePerLogicalCall proves a retried call
// bills once. The wrapped client records its charge only after a successful
// round trip, so two rate limits leave exactly one charge.
func TestRetryingTranslatorBillsOneChargePerLogicalCall(t *testing.T) {
	ledger := cost.NewLedger()
	inner := &billingFlakyTranslator{rec: ledger, errs: []error{rateLimitErr(), rateLimitErr()}, reply: "text"}
	tr := NewRetryingTranslator(inner, fastRetryPolicy(3))
	got, err := tr.Translate(context.Background(), gemini.TranslateRequest{SegmentID: 5, Text: "line"})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if got != "text" {
		t.Errorf("text = %q, want the reply", got)
	}
	if inner.count() != 3 {
		t.Errorf("calls = %d, want 3", inner.count())
	}
	charges := ledger.Charges()
	if len(charges) != 1 {
		t.Fatalf("charges = %d, want exactly one for the successful call", len(charges))
	}
	if charges[0].TakeID != 5 {
		t.Errorf("charge take = %d, want line 5", charges[0].TakeID)
	}
}

// TestNewRetryingTranslatorKeepsNil proves a nil client stays nil, so the
// pipeline still reports a missing translator.
func TestNewRetryingTranslatorKeepsNil(t *testing.T) {
	if tr := NewRetryingTranslator(nil, DefaultRetryPolicy()); tr != nil {
		t.Errorf("NewRetryingTranslator(nil) = %T, want nil", tr)
	}
	if syn := NewRetryingSynthesizer(nil, DefaultRetryPolicy()); syn != nil {
		t.Errorf("NewRetryingSynthesizer(nil) = %T, want nil", syn)
	}
	if seg := NewRetryingSegmenter(nil, DefaultRetryPolicy()); seg != nil {
		t.Errorf("NewRetryingSegmenter(nil) = %T, want nil", seg)
	}
}

// TestMarkUpstreamBusyTagsOnlyTransientFailures proves the run route learns a
// busy upstream and never sees an internal failure as busy.
func TestMarkUpstreamBusyTagsOnlyTransientFailures(t *testing.T) {
	busy := markUpstreamBusy(fmt.Errorf("segment media: %w", rateLimitErr()))
	if !errors.Is(busy, api.ErrUpstreamBusy) {
		t.Errorf("busy error = %v, want the busy marker", busy)
	}
	if !IsTransientUpstream(busy) {
		t.Errorf("busy error = %v, want the cause kept for the log", busy)
	}
	internal := markUpstreamBusy(errors.New("disk full"))
	if errors.Is(internal, api.ErrUpstreamBusy) {
		t.Errorf("internal error = %v, want no busy marker", internal)
	}
}
