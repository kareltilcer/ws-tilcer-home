package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// cachingIntrospector caches active introspection results for a token's
// remaining TTL (bounded by ttlCap). This keeps introspection off every request
// — critical because the dashboard is the landing route (PRD D2). The cache is
// keyed by a SHA-256 of the token, never the raw token.
type cachingIntrospector struct {
	inner  Introspector
	ttlCap time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	claims  Claims
	expires time.Time
}

// NewCachingIntrospector wraps inner with a TTL cache. ttlCap bounds how long an
// otherwise longer-lived token may be cached (tokens live ~15 minutes).
func NewCachingIntrospector(inner Introspector, ttlCap time.Duration) Introspector {
	return &cachingIntrospector{
		inner:   inner,
		ttlCap:  ttlCap,
		now:     time.Now,
		entries: make(map[string]cacheEntry),
	}
}

func (c *cachingIntrospector) Introspect(ctx context.Context, token string) (Claims, error) {
	key := hashToken(token)
	now := c.now()

	c.mu.Lock()
	if e, ok := c.entries[key]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		return e.claims, nil
	}
	c.mu.Unlock()

	claims, err := c.inner.Introspect(ctx, token)
	if err != nil {
		return Claims{}, err
	}
	// Only cache active results; never cache a negative for long (a token could
	// be refreshed a moment later).
	if claims.Active {
		expires := now.Add(c.ttlCap)
		if !claims.ExpiresAt.IsZero() && claims.ExpiresAt.Before(expires) {
			expires = claims.ExpiresAt
		}
		c.mu.Lock()
		c.entries[key] = cacheEntry{claims: claims, expires: expires}
		c.sweepLocked(now)
		c.mu.Unlock()
	}
	return claims, nil
}

// sweepLocked drops expired entries; called opportunistically on writes so the
// map cannot grow without bound. Caller holds c.mu.
func (c *cachingIntrospector) sweepLocked(now time.Time) {
	for k, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, k)
		}
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
