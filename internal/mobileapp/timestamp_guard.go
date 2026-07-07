package mobileapp

// applyByTimestamp reports whether an incoming write should overwrite the stored
// value under timestamp last-write-wins (mp-y3nk). incoming/stored are
// server-relative unix-millis (learned by clients via GET /api/time); 0 means
// unstamped/legacy.
//
// An unstamped incoming (0) always applies, and any write against an unstamped
// stored value (0) applies: so clients that do not stamp keep the previous
// arrival-order behavior and legacy on-disk results are never blocked. The
// timestamp comparison only governs when BOTH sides are stamped, where a
// newer-or-equal incoming applies and a strictly older one is dropped (an
// equal timestamp applies so a reconnect replay of the same change is
// idempotent rather than rejected).
//
// This is the timestamp layer only; the completed-never-reverted regression
// guard and the running rev-guard remain in force ON TOP of it, so a stale
// running write can never revert a finished match regardless of its timestamp.
func applyByTimestamp(incoming, stored int64) bool {
	if incoming == 0 || stored == 0 {
		return true
	}
	return incoming >= stored
}
