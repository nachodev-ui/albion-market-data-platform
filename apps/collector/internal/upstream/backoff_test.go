package upstream

import (
	"testing"
	"time"
)

func TestRetryBackoffWithJitterIsStableAndBounded(t *testing.T) {
	base := time.Second
	got := retryBackoffWithJitter(base, time.Minute, 3, "request-a")
	if got != retryBackoffWithJitter(base, time.Minute, 3, "request-a") {
		t.Fatal("jitter must be stable for the same request and attempt")
	}
	raw := retryBackoff(base, time.Minute, 3)
	minimum := raw * 80 / 100
	maximum := raw * 120 / 100
	if got < minimum || got > maximum {
		t.Fatalf("jittered=%s outside [%s,%s]", got, minimum, maximum)
	}
}

func TestRetryAttemptDelayHonorsMaximum(t *testing.T) {
	got := retryAttemptDelay(10*time.Second, 15*time.Second, 10, "request-b")
	if got > 15*time.Second {
		t.Fatalf("delay=%s", got)
	}
	if got < 12*time.Second {
		t.Fatalf("delay=%s below bounded jitter floor", got)
	}
}

func TestRetryJitterSpreadsDifferentRequests(t *testing.T) {
	first := retryBackoffWithJitter(time.Second, time.Minute, 4, "request-a")
	second := retryBackoffWithJitter(time.Second, time.Minute, 4, "request-b")
	if first == second {
		t.Fatalf("expected different jitter: %s", first)
	}
}
