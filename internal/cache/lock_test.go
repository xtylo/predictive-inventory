package cache

import (
	"testing"
	"time"
)

func TestTryLock(t *testing.T) {
	c, server := newTestCache(t)
	ctx := t.Context()
	const ttl = time.Minute

	mustLock := func(want bool, when string) {
		t.Helper()
		got, err := c.TryLock(ctx, "job", ttl)
		if err != nil {
			t.Fatalf("TryLock() error %s: %v", when, err)
		}
		if got != want {
			t.Errorf("TryLock() = %v %s, want %v", got, when, want)
		}
	}

	mustLock(true, "on a free lock")
	mustLock(false, "while it is held")

	server.FastForward(ttl + time.Second)
	mustLock(true, "after the TTL expired")

	if err := c.Unlock(ctx, "job"); err != nil {
		t.Fatalf("Unlock() error: %v", err)
	}
	mustLock(true, "after Unlock")
}
