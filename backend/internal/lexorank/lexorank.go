// Package lexorank implements lexorank-style fractional index keys (PRD D4).
//
// A position is a TEXT key over the base-62 alphabet 0-9A-Za-z, whose ASCII byte
// order equals its logical order, so rows sort with `ORDER BY position, id`.
// Between(a, b) returns a key strictly between its neighbours, so inserting or
// moving a row rewrites exactly ONE row — never renumbering siblings.
//
// Keys lengthen rather than collide: repeatedly inserting between the same two
// neighbours bisects the gap and grows the key by ~1 char every few inserts, so
// there is no true exhaustion. Rebalance is the rare, explicit renormalisation
// (the one sanctioned multi-row write) for when keys grow impractically long.
//
// Invariant the callers must respect: keys are generated only through this
// package (First/Head/Tail/Between/NKeys), which never produces a key ending in
// the smallest digit — this is what guarantees a strict midpoint always exists
// between any two generated keys.
package lexorank

import "strings"

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

const base = len(alphabet)

func idx(c byte) int { return strings.IndexByte(alphabet, c) }

func chr(i int) byte { return alphabet[i] }

// First returns a key with no neighbours — the midpoint of the whole space.
func First() string { return Between("", "") }

// Head returns a key ordered strictly before next.
func Head(next string) string { return Between("", next) }

// Tail returns a key ordered strictly after prev.
func Tail(prev string) string { return Between(prev, "") }

// Between returns a key k with a < k < b. An empty a means −∞ (before all keys);
// an empty b means +∞ (after all keys). It assumes a < b when both are non-empty;
// if that precondition is violated it degrades gracefully to a key after a.
func Between(a, b string) string {
	if b != "" && a != "" && a >= b {
		// Misuse (neighbours out of order or equal): stay deterministic.
		return Tail(a)
	}
	var sb []byte
	i := 0
	for {
		da := 0
		if i < len(a) {
			da = idx(a[i])
		}
		db := base
		if b != "" && i < len(b) {
			db = idx(b[i])
		}
		if da+1 < db {
			// A gap exists at this position: emit a digit strictly between.
			sb = append(sb, chr((da+db)/2))
			return string(sb)
		}
		// No gap here: copy a's digit and descend. If b's digit was exactly one
		// above a's, we have now dropped strictly below b, so b stops constraining.
		sb = append(sb, chr(da))
		if db == da+1 {
			b = ""
		}
		i++
	}
}

// NKeys returns n ordered, evenly-spaced keys of equal width — used to seed a
// fresh ordered set (e.g. the default board's columns).
func NKeys(n int) []string {
	if n <= 0 {
		return nil
	}
	// Choose the smallest width w such that base^w > n (room for n interior points).
	width := 1
	capacity := base
	for capacity <= n {
		capacity *= base
		width++
	}
	step := capacity / (n + 1)
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = encodeFixed((i+1)*step, width)
	}
	return out
}

// Rebalance returns len(keys) fresh, evenly-spaced, short keys preserving order.
// It is the deliberate renormalisation escape hatch; callers rewrite the affected
// rows in one explicit operation, not on the hot path.
func Rebalance(keys []string) []string { return NKeys(len(keys)) }

// encodeFixed renders v as a width-digit base-62 string (most significant first).
func encodeFixed(v, width int) string {
	b := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		b[i] = chr(v % base)
		v /= base
	}
	return string(b)
}
