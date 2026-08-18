package gh

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryAttemptsSucceedsAfterRateLimit(t *testing.T) {
	calls := 0
	out, err := retryAttempts(context.Background(), 3, []time.Duration{time.Millisecond, time.Millisecond}, func() (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("gh issue list: HTTP 429: You have exceeded a secondary rate limit")
		}
		return "hello", nil
	})
	if err != nil {
		t.Fatalf("retryAttempts: %v", err)
	}
	if out != "hello" {
		t.Errorf("out = %q, want hello", out)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetryAttemptsGivesUp(t *testing.T) {
	calls := 0
	_, err := retryAttempts(context.Background(), 3, []time.Duration{time.Millisecond, time.Millisecond}, func() (string, error) {
		calls++
		return "", errors.New("gh issue list: HTTP 429: API rate limit exceeded")
	})
	if err == nil {
		t.Fatal("retryAttempts = nil, want a rate-limit error")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetryAttemptsPassesThroughOtherErrors(t *testing.T) {
	calls := 0
	_, err := retryAttempts(context.Background(), 3, []time.Duration{time.Second, time.Second}, func() (string, error) {
		calls++
		return "", errors.New("gh label create: HTTP 403: Resource not accessible by integration")
	})
	if err == nil {
		t.Fatal("retryAttempts = nil, want error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRetryAttemptsStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := retryAttempts(ctx, 3, []time.Duration{time.Hour, time.Hour}, func() (string, error) {
		calls++
		return "", errors.New("gh issue list: HTTP 429: API rate limit exceeded")
	})
	if err != context.Canceled {
		t.Fatalf("retryAttempts error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no wait after cancel)", calls)
	}
}

func TestIsRateLimited(t *testing.T) {
	for _, tc := range []struct {
		err  string
		want bool
	}{
		{"gh issue list: HTTP 429: API rate limit exceeded", true},
		{"gh issue list: HTTP 403: You have exceeded a secondary rate limit", true},
		{"gh pr create: HTTP 429: too many requests", true},
		{"gh issue edit: HTTP 403: Resource not accessible by integration", false},
		{"gh label create: label already exists", false},
	} {
		if got := isRateLimited(errors.New(tc.err)); got != tc.want {
			t.Errorf("isRateLimited(%q) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
