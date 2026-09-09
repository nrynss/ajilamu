package fit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"syscall"
	"time"

	"google.golang.org/genai"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/gemini"
	"github.com/nrynss/ajilamu/internal/tts"
	"github.com/nrynss/ajilamu/internal/types"
)

// Default retry bounds. A transient upstream call makes three calls at most,
// so it retries twice. The delay starts at a quarter second, doubles, and
// never exceeds two seconds.
const (
	// DefaultRetryAttempts caps the calls one logical call makes.
	DefaultRetryAttempts = 3
	// DefaultRetryBaseDelay is the first backoff delay.
	DefaultRetryBaseDelay = 250 * time.Millisecond
	// DefaultRetryMaxDelay caps one backoff delay.
	DefaultRetryMaxDelay = 2 * time.Second
)

// RetryPolicy bounds the calls one logical upstream call makes when a
// transient failure interrupts it. A zero field takes its default, so the zero
// RetryPolicy equals DefaultRetryPolicy.
type RetryPolicy struct {
	// MaxAttempts caps the calls including the first. One means no retry.
	MaxAttempts int
	// BaseDelay is the first backoff delay. Each later delay doubles it.
	BaseDelay time.Duration
	// MaxDelay caps one backoff delay.
	MaxDelay time.Duration
}

// DefaultRetryPolicy returns the production retry bounds.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: DefaultRetryAttempts,
		BaseDelay:   DefaultRetryBaseDelay,
		MaxDelay:    DefaultRetryMaxDelay,
	}
}

// normalized fills every unset field and keeps MaxDelay at or above BaseDelay.
func (p RetryPolicy) normalized() RetryPolicy {
	out := DefaultRetryPolicy()
	if p.MaxAttempts > 0 {
		out.MaxAttempts = p.MaxAttempts
	}
	if p.BaseDelay > 0 {
		out.BaseDelay = p.BaseDelay
	}
	if p.MaxDelay > 0 {
		out.MaxDelay = p.MaxDelay
	}
	if out.MaxDelay < out.BaseDelay {
		out.MaxDelay = out.BaseDelay
	}
	return out
}

// backoff returns the delay before the retry that follows attempt n, which
// starts at one. The delay doubles with n and stops at MaxDelay. Equal jitter
// then spreads it over the second half of that window, so a retry never waits
// longer than MaxDelay and never collapses to zero.
func (p RetryPolicy) backoff(attempt int) time.Duration {
	delay := p.BaseDelay
	for i := 1; i < attempt; i++ {
		if delay >= p.MaxDelay {
			break
		}
		delay *= 2
		if delay > p.MaxDelay {
			delay = p.MaxDelay
		}
	}
	if delay <= 1 {
		return delay
	}
	half := delay / 2
	return half + rand.N(half)
}

// IsTransientUpstream reports whether err is a temporary upstream failure a
// caller may retry. A rate limit, an unavailable service, and a reset or timed
// out connection are transient. A rejected request and a failed authentication
// are not. A cancelled or expired context is never transient, because the
// caller ended the work.
func IsTransientUpstream(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		return transientHTTPStatus(apiErr.Code)
	}
	var apiPtr *genai.APIError
	if errors.As(err, &apiPtr) && apiPtr != nil {
		return transientHTTPStatus(apiPtr.Code)
	}
	switch status.Code(err) {
	case codes.ResourceExhausted, codes.Unavailable:
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	switch {
	case errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, io.ErrUnexpectedEOF):
		return true
	}
	return false
}

// transientHTTPStatus reports whether one HTTP status names a temporary
// upstream condition. 429 is a rate limit and 503 is an unavailable service.
func transientHTTPStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusServiceUnavailable
}

// markUpstreamBusy tags an exhausted transient upstream failure with
// api.ErrUpstreamBusy, so the run route reports a plain busy sentence. The
// cause stays in the chain for the server log.
func markUpstreamBusy(err error) error {
	if !IsTransientUpstream(err) {
		return err
	}
	return fmt.Errorf("%w: %w", api.ErrUpstreamBusy, err)
}

// retryTransient calls fn until it succeeds, the context ends, or fn reports a
// non-transient error. It stops after the bounded attempts and returns the
// last error.
func retryTransient(ctx context.Context, p RetryPolicy, fn func() error) error {
	policy := p.normalized()
	var err error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		err = fn()
		if err == nil {
			return nil
		}
		if !IsTransientUpstream(err) {
			return err
		}
		if attempt == policy.MaxAttempts {
			return err
		}
		if waitErr := waitBackoff(ctx, policy.backoff(attempt)); waitErr != nil {
			return waitErr
		}
	}
	return err
}

// retryTransientValue is retryTransient for a call that returns a value. The
// value of the successful call survives, and a failed call leaves the zero
// value.
func retryTransientValue[T any](ctx context.Context, p RetryPolicy, fn func() (T, error)) (T, error) {
	var out T
	err := retryTransient(ctx, p, func() error {
		value, callErr := fn()
		if callErr == nil {
			out = value
		}
		return callErr
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// waitBackoff sleeps for delay and aborts when the context ends first.
func waitBackoff(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryingTranslator retries the transient upstream failures of one translator.
type retryingTranslator struct {
	inner  gemini.Translator
	policy RetryPolicy
}

// NewRetryingTranslator wraps a translator with bounded retries for transient
// upstream failures. A nil client stays nil, so the pipeline still reports a
// missing translator. The wrapped client records its charge only after a
// successful round trip, so a retried call bills once and a call that already
// billed is never retried.
func NewRetryingTranslator(inner gemini.Translator, policy RetryPolicy) gemini.Translator {
	if inner == nil {
		return nil
	}
	return &retryingTranslator{inner: inner, policy: policy}
}

// Translate retries one translation call.
func (t *retryingTranslator) Translate(ctx context.Context, req gemini.TranslateRequest) (string, error) {
	return retryTransientValue(ctx, t.policy, func() (string, error) {
		return t.inner.Translate(ctx, req)
	})
}

// SetRecorder forwards to the wrapped translator when it accepts a recorder.
func (t *retryingTranslator) SetRecorder(rec ChargeRecorder) {
	if rs, ok := t.inner.(RecorderSetter); ok {
		rs.SetRecorder(rec)
	}
}

// retryingSynthesizer retries the transient upstream failures of one
// synthesizer.
type retryingSynthesizer struct {
	inner  tts.Synthesizer
	policy RetryPolicy
}

// NewRetryingSynthesizer wraps a synthesizer with bounded retries for transient
// upstream failures. A nil client stays nil. The wrapped client records its
// charge only after it writes the take, so a retried call bills once.
func NewRetryingSynthesizer(inner tts.Synthesizer, policy RetryPolicy) tts.Synthesizer {
	if inner == nil {
		return nil
	}
	return &retryingSynthesizer{inner: inner, policy: policy}
}

// Synthesize retries one synthesis call.
func (s *retryingSynthesizer) Synthesize(ctx context.Context, req tts.SynthesizeRequest) error {
	return retryTransient(ctx, s.policy, func() error {
		return s.inner.Synthesize(ctx, req)
	})
}

// SetRecorder forwards to the wrapped synthesizer when it accepts a recorder.
func (s *retryingSynthesizer) SetRecorder(rec ChargeRecorder) {
	if rs, ok := s.inner.(RecorderSetter); ok {
		rs.SetRecorder(rec)
	}
}

// retryingSegmenter retries the transient upstream failures of one segmenter.
type retryingSegmenter struct {
	inner  gemini.Segmenter
	policy RetryPolicy
}

// NewRetryingSegmenter wraps a segmenter with bounded retries for transient
// upstream failures. A nil client stays nil. The wrapped client records its
// charge only after a successful parse, so a retried call bills once.
func NewRetryingSegmenter(inner gemini.Segmenter, policy RetryPolicy) gemini.Segmenter {
	if inner == nil {
		return nil
	}
	return &retryingSegmenter{inner: inner, policy: policy}
}

// Segment retries one segmentation call.
func (s *retryingSegmenter) Segment(ctx context.Context, in gemini.Input) ([]types.Segment, error) {
	return retryTransientValue(ctx, s.policy, func() ([]types.Segment, error) {
		return s.inner.Segment(ctx, in)
	})
}

// SetRecorder forwards to the wrapped segmenter when it accepts a recorder.
func (s *retryingSegmenter) SetRecorder(rec ChargeRecorder) {
	if rs, ok := s.inner.(RecorderSetter); ok {
		rs.SetRecorder(rec)
	}
}
