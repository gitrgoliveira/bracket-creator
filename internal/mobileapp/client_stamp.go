package mobileapp

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// modifiedAtRefuseSkewMs is the margin, in milliseconds, inside which a
// client-supplied server-relative ModifiedAt stamped in the FUTURE is still
// honoured as-is. A legitimate write is stamped at action time (at or before
// "now" in the server frame), so anything ahead of the server is either honest
// residual drift in the client's learned offset or plain request jitter between
// the stamp and this evaluation; 5 seconds covers both generously.
//
// Beyond it the stamp is provably wrong, and BOTH of the ways a server can
// "accept" it lose data:
//   - honouring it lets the write beat every legitimate later write until real
//     time catches up with the stamp, freezing the match for that whole window;
//   - zeroing it (what this code used to do, with a 2s threshold) takes
//     ApplyByTimestamp's unstamped bypass, so the write applies
//     UNCONDITIONALLY and silently overwrites a NEWER stored result. A device
//     whose clock runs ahead therefore won every race it entered, and a brief
//     network drop is all it takes to enter one.
//
// So past the margin the write is REFUSED and reported (respondClockSkew), the
// only answer that neither guesses nor discards: the operator is told, and the
// device is expected to resync its clock and re-enter the result.
//
// Negative stamps are NOT skew, they are garbage (a bad offset subtraction, a
// pre-epoch clock), and they keep the legacy zero-then-bypass: there is no
// "correct" write being fenced out, and refusing them would newly break clients
// that have always been silently tolerated.
const modifiedAtRefuseSkewMs = 5 * 1000

// clientClockSkew classifies a client-supplied ModifiedAt against the server
// clock for the timestamp last-write-wins guard (mp-y3nk, bc-cse). It is the
// VALIDATE half of the pair; clampClientModifiedAt is the clamp half.
//
// It returns the server "now" it judged against (echoed to the client as
// diagnostic data, see respondClockSkew), how far ahead of that the stamp is,
// and whether that is beyond modifiedAtRefuseSkewMs. Split from the clamp
// because the three write endpoints must be able to tell "refuse this write"
// from "use this stamp": folding the two together is exactly the bug, since the
// only in-band way to say "unusable" was 0, and 0 means "apply unconditionally".
//
// An unstamped (0) or negative value is never a refusal: aheadMs is reported
// relative to the server clock but refuse stays false, and the clamp then turns
// it into the unstamped bypass as it always has.
func clientClockSkew(v int64) (serverNowMs, aheadMs int64, refuse bool) {
	serverNowMs = time.Now().UnixMilli()
	if v <= 0 {
		return serverNowMs, 0, false
	}
	aheadMs = v - serverNowMs
	return serverNowMs, aheadMs, aheadMs > modifiedAtRefuseSkewMs
}

// clampClientModifiedAt sanitises a client-supplied ModifiedAt for the
// timestamp last-write-wins guard (mp-y3nk). A negative value is untrustworthy
// garbage and falls back to 0, which the guard treats as unstamped
// (arrival-order) and is always safe.
//
// Future stamps are NO LONGER clamped here: a small-margin future stamp is
// honoured as-is, and one beyond modifiedAtRefuseSkewMs is refused by the
// caller before this is ever reached (clientClockSkew). Clamping a far-future
// stamp to 0 looked conservative and was the opposite, because 0 is the
// unconditional-apply bypass.
func clampClientModifiedAt(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// logClockSkewRefusal records a write refused because the client's stamp was
// beyond modifiedAtRefuseSkewMs ahead of the server clock.
//
// LOGGED for the same reason respondSuperseded logs: this is a successful-
// looking response that throws away work an operator typed in, and it leaves no
// trace in the match file by definition (nothing was written). When a court
// reports "my score vanished" this line, with how far ahead that device's clock
// was, is the only server-side record that says which device to fix.
//
// Separate from respondClockSkew because bulk-score reports its refusals
// per-entry inside an overall 200 rather than as a whole-response body, so it
// cannot use the responder but must still leave the same breadcrumb.
func logClockSkewRefusal(c *gin.Context, aheadMs int64, matchID string) {
	target := c.Request.URL.Path
	if matchID != "" {
		target = fmt.Sprintf("%s (match %s)", target, matchID)
	}
	log.Printf("mobileapp: %s %s: write refused, client modifiedAt is %dms ahead of the server clock (margin %dms); nothing was written",
		c.Request.Method, target, aheadMs, modifiedAtRefuseSkewMs)
}

// respondClockSkew writes the shared body for a write refused because the
// client's modifiedAt is implausibly far in the future (bc-cse). Modelled on
// respondSuperseded, and for the same reasons: ONE body in ONE place, because a
// body hand-copied per handler is how the client's single branch quietly stops
// matching one of them.
//
// 200 with applied:false, never a 4xx/5xx. The SPA's offline write queue retries
// 5xx forever (the mp-q8c6 poisoned-queue pattern), and a skewed write can never
// win a retry while the clock stays wrong, so an error status here would wedge
// the queue behind an unwinnable request. `applied:false` is the shape the
// client already keys on for superseded; `reason` is what distinguishes the two,
// and the remedy differs: superseded means someone else's result won, clock_skew
// means this device's clock is wrong and the SAME result should be entered again
// once it has resynced.
//
// serverNowMs is DIAGNOSTIC ONLY. It is the server clock at the moment of the
// verdict, for the client's log and for a human comparing the two devices. It
// must NOT be applied directly as a clock offset: a one-way response carries no
// round-trip measurement, so adopting it as an offset would bake the request's
// latency into the client's frame. The client relearns via GET /api/time, which
// is RTT-corrected.
func respondClockSkew(c *gin.Context, serverNowMs, aheadMs int64) {
	logClockSkewRefusal(c, aheadMs, "")
	c.JSON(http.StatusOK, gin.H{
		"applied":     false,
		"reason":      "clock_skew",
		"serverNowMs": serverNowMs,
		"message":     "Nothing was changed: this device's clock appears to be ahead of the server. The app is resyncing its clock; if it does not go through by itself, do it again.",
	})
}

// judgeClientStamp judges the {modifiedAt} a stamped write's body carries (a
// check-in, a court call, a clear) and reports whether the write may go on. A
// missing or non-positive stamp is 400, naming what it stands for: every
// caller is the outbox, which stamps each write, and an unstamped one could
// never be ordered against a stored one. A stamp beyond the clock skew margin
// gets the clock_skew answer the outbox re-stamps on (clientClockSkew and
// respondClockSkew, the score door's pair).
func judgeClientStamp(c *gin.Context, modifiedAt int64, what string) (int64, bool) {
	if modifiedAt <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "modifiedAt is required: " + what})
		return 0, false
	}
	if serverNowMs, aheadMs, refuse := clientClockSkew(modifiedAt); refuse {
		respondClockSkew(c, serverNowMs, aheadMs)
		return 0, false
	}
	return clampClientModifiedAt(modifiedAt), true
}
