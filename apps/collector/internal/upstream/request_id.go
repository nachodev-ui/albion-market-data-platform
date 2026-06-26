package upstream

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"time"
)

var requestIDFallbackCounter atomic.Uint64

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		binary.BigEndian.PutUint64(value[0:8], uint64(time.Now().UTC().UnixNano()))
		binary.BigEndian.PutUint64(value[8:16], requestIDFallbackCounter.Add(1))
	}
	// RFC 4122 version 4 / variant 1. The central API accepts both compact and
	// hyphenated UUIDs; the standard shape is easier to inspect in logs.
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	)
}
