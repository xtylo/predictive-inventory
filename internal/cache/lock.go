package cache

import (
	"context"
	"fmt"
	"time"
)

func lockKey(name string) string {
	return "lock:" + name
}

// TryLock acquires the lock called name for ttl, without waiting.
// It returns false when another holder already has it.
//
// SET NX ("set if not exists") is atomic in Redis, so when many workers,
// or many service instances, race for the same name, exactly one wins.
// The TTL guarantees the lock disappears even if its holder crashes.
func (c *Cache) TryLock(ctx context.Context, name string, ttl time.Duration) (bool, error) {
	acquired, err := c.rdb.SetNX(ctx, lockKey(name), 1, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("acquire lock %s: %w", name, err)
	}
	return acquired, nil
}

// Unlock releases the lock called name before its TTL expires.
func (c *Cache) Unlock(ctx context.Context, name string) error {
	if err := c.rdb.Del(ctx, lockKey(name)).Err(); err != nil {
		return fmt.Errorf("release lock %s: %w", name, err)
	}
	return nil
}
