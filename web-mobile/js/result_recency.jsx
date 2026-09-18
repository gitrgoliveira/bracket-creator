// result_recency.jsx: which completed bout is the most recent RESULT.
//
// One owner, because two surfaces answer the same operator question and used
// to answer it differently (mp-jnvl): the court console's context strip ("the
// bout just played") and the public viewer's Recent results. Both previously
// read schedule order as recency, so on a court running out of schedule order
// they named different bouts as the latest.
//
// `modifiedAt` is the stamp every recordScore write carries (mp-y3nk), so it
// is the time of the last WRITE, not of the result: STARTING a bout is such a
// write, so a bout carries its start stamp until its score write replaces it.
// A write that sends no stamp inherits the stored one server-side (engine
// applyPoolWrite), so a bout started and then withdrawn keeps its start stamp.
//
// A bout is unstamped only when nothing ever stamped it: a file predating the
// field, or a match closed while still `scheduled`. Those fall back to
// scheduled time, which is all they carry, so an all-unstamped list keeps
// exactly the ordering both surfaces had before.
//
// A leaf with no imports: keep it that way, so every surface can take the rule
// without taking a dependency graph with it.

// Newest result first. Stamped bouts outrank unstamped ones (a stamp is
// positive evidence of when the result landed; scheduled time is not), and
// equal stamps fall back to the later slot.
export function resultRecencyDesc(a, b) {
    // A missing field is ordinary (an unstamped bout, an untimed row); a
    // missing MATCH is not, so there is no null guard here: both callers sort
    // a filtered list of match objects.
    const ta = Number(a.modifiedAt) || 0;
    const tb = Number(b.modifiedAt) || 0;
    if (ta !== tb) return tb - ta;
    return (b.scheduledAt || "").localeCompare(a.scheduledAt || "");
}

// The `count` most recently played bouts, returned in the CALLER's order so a
// list already ordered for display keeps reading the way it did; only which
// bouts appear changes.
export function recentlyPlayed(completed, count) {
    if (completed.length <= count) return completed;
    const keep = new Set(
        completed
            .map((m, i) => ({ m, i }))
            .sort((x, y) => resultRecencyDesc(x.m, y.m) || (y.i - x.i))
            .slice(0, count)
            .map((e) => e.i)
    );
    return completed.filter((_, i) => keep.has(i));
}

// The single bout just played, for a panel that anchors to one result.
export function mostRecentlyPlayed(completed) {
    return recentlyPlayed(completed, 1)[0] || null;
}
