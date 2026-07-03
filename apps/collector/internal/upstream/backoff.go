package upstream

import (
	"encoding/binary"
	"hash/fnv"
	"time"
)

const retryJitterPercent = 20

func retryBackoffWithJitter(base, maximum time.Duration, attempts int, requestID string) time.Duration {
	return applyRetryJitter(retryBackoff(base, maximum, attempts), maximum, requestID, attempts)
}

func retryAttemptDelay(base, maximum time.Duration, attempt int, requestID string) time.Duration {
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(attempt) * base
	if maximum > 0 && delay > maximum {
		delay = maximum
	}
	return applyRetryJitter(delay, maximum, requestID, attempt)
}

func applyRetryJitter(delay, maximum time.Duration, requestID string, attempt int) time.Duration {
	if delay <= 0 {
		return 0
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(requestID))
	var attemptBytes [8]byte
	binary.LittleEndian.PutUint64(attemptBytes[:], uint64(maxInt(attempt, 0)))
	_, _ = hasher.Write(attemptBytes[:])

	// Map the stable hash to [-1, 1], then apply a bounded +/-20% spread.
	unit := float64(hasher.Sum64()%20001)/10000 - 1
	window := float64(delay) * retryJitterPercent / 100
	result := delay + time.Duration(unit*window)
	minimum := delay * (100 - retryJitterPercent) / 100
	if result < minimum {
		result = minimum
	}
	if maximum > 0 && result > maximum {
		result = maximum
	}
	if result < time.Millisecond {
		return time.Millisecond
	}
	return result
}
