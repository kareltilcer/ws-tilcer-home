package ws

// Test seam for the v10 per-user index (D232).
//
// `byUser` is unexported and must stay that way — nothing outside the hub has any
// business holding a *client. But the ONE bug this index can have is invisible
// from the outside: a `remove` that forgets the second map leaks a dead client per
// disconnect, and every externally observable behaviour (Publish, PublishTo,
// Count) stays correct while it does. So the leak test asserts against the map
// itself, which means the map has to be readable from a test.
//
// The external ws_test package is what the rest of the suite uses; these two
// helpers are compiled only into the test binary.

// TrackedClientsForTest returns how many live connections are indexed under
// userID. A disconnected client that is still counted here is the leak.
func (h *Hub) TrackedClientsForTest(userID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.byUser[userID])
}

// TrackedUsersForTest returns how many user ids the index holds at all. An id
// whose set has emptied must be gone entirely, not left behind as an empty map.
func (h *Hub) TrackedUsersForTest() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.byUser)
}
