// SSE patch-apply logic, centralised.
//
// app.jsx and admin.jsx both used to carry an identical
// patchCompetitionData(prev, event) implementation that walked the
// competition's poolMatches and bracket.rounds, looked up each match by
// id in the SSE event payload, and merged via mergeMatchPatch (declared
// in data.jsx). This module is the single source of truth for that
// logic — both app.jsx and admin.jsx now import it.
//
// The match-update SSE payload is shaped as either:
//   { result: { id, ipponsA, ipponsB, hansokuA, hansokuB, ... } }
//   { results: [ { id, ... }, ... ] }
// We accept both forms so a future server bulk-update doesn't require
// a frontend release. results[] takes precedence over result if both
// are present.
//
// Bracket matches store ippons as joined strings (scoreA / scoreB)
// rather than arrays, because the bracket renderer reads the joined
// form. We map ipponsA/B → scoreA/B before calling mergeMatchPatch
// for bracket entries; pool matches don't need that translation
// because their renderers read ipponsA/B directly.
//
// Returns the unchanged `prev` reference if no listed result IDs
// matched any match in the competition. This avoids unnecessary
// re-renders downstream when a competition receives an SSE event for
// a sibling competition's match (the caller is expected to pre-filter
// by competitionId, but defensive identity-preservation here is cheap).
//
// FR-025 hook (queue-position recompute): listeners that need to react
// to status transitions (live/scheduled → completed) can compose with
// applyPatch by reading the merged result and recomputing derived
// fields. Slice 1's T049 will add a queue-position invalidator that
// runs on the same event before applyPatch returns.

// Direct ESM import of the real merge helper instead of the previous
// window.mergeMatchPatch / spread-fallback dance. The spread fallback
// silently dropped court/scheduledAt preservation when data.jsx hadn't
// loaded yet (a test-time hazard, since production index.html ordering
// always loaded data.js first). The explicit import makes the
// dependency a build-graph contract — esbuild will fail loudly rather
// than fall through to a wrong-shape merge.
import { mergeMatchPatch as _mergeMatchPatch } from './data.jsx';

// statusSortOrder mirrors annotateBracketQueuePositions in
// internal/mobileapp/handlers_match.go and the per-court sort in
// ScheduleViewer (viewer.jsx). Running ranks ahead of scheduled, which
// ranks ahead of completed/everything-else.
const statusSortOrder = (s) => (s === "running" ? 0 : s === "scheduled" ? 1 : 2);

// Stable per-court ordering: gather pointers to the original entries,
// sort them by (status priority, scheduledAt, original index) without
// mutating the input array. The result is a flat list of [origIndex,
// match] pairs in display order — caller iterates and increments a
// per-court counter to derive queue positions.
function _orderByCourtKey(entries) {
    // `entries` is [{ idx, m, court }]; we sort within each court so
    // the per-court counter increments in viewer-visible order.
    const byCourt = new Map();
    for (const e of entries) {
        const arr = byCourt.get(e.court) || [];
        arr.push(e);
        byCourt.set(e.court, arr);
    }
    for (const arr of byCourt.values()) {
        arr.sort((a, b) => {
            const oa = statusSortOrder(a.m.status);
            const ob = statusSortOrder(b.m.status);
            if (oa !== ob) return oa - ob;
            const sa = a.m.scheduledAt || "99:99";
            const sb = b.m.scheduledAt || "99:99";
            if (sa !== sb) return sa < sb ? -1 : 1;
            return a.idx - b.idx;
        });
    }
    return byCourt;
}

// Recompute queuePosition on scheduled poolMatches per court after an
// SSE patch transitions a match to completed. The backend recomputes
// server-side on the next GET (see internal/state/match.go), but SSE
// patches only carry the single updated match — without this client-side
// step the UI would still show stale "3 before yours" labels until the
// next viewer refresh. FR-025, R3.
//
// Algorithm mirrors annotateBracketQueuePositions in handlers_match.go:
// gather per-court entries, sort by (status priority, scheduledAt) so
// the counter increments in viewer-display order, then assign 1, 2,
// 3, … to scheduled matches. Running/completed get 0. This handles
// the case where an SSE patch moves a match's court/scheduledAt and
// the array order no longer reflects the displayed order.
//
// Touches only matches whose existing queuePosition differs from the
// recomputed value, preserving object identity for unaffected matches
// (matters for React.memo on VSchedItem / TWMatch).
function recomputeQueuePositions(matches) {
    if (!matches || matches.length === 0) return matches;
    // If the server's payload never populated queuePosition (older
    // backends or non-annotated views), do nothing — there's no UI
    // baseline to keep in sync and assigning positions risks adding a
    // field the UI doesn't currently render. Avoids gratuitous identity
    // churn for memoised match-card components.
    const hasAnyQueuePosition = matches.some(m => m.queuePosition !== undefined);
    if (!hasAnyQueuePosition) return matches;

    // Build per-court ordered buckets, then derive positions by court
    // and write them back into a positions-by-index lookup.
    const entries = matches.map((m, idx) => ({ idx, m, court: m.court || "" }));
    const byCourt = _orderByCourtKey(entries);
    const newPositions = new Array(matches.length).fill(0);
    for (const bucket of byCourt.values()) {
        let counter = 0;
        for (const e of bucket) {
            if (e.m.status === "scheduled") {
                counter++;
                newPositions[e.idx] = counter;
            }
        }
    }

    let touched = false;
    const next = matches.map((m, idx) => {
        const pos = newPositions[idx];
        if ((m.queuePosition || 0) !== pos) {
            touched = true;
            return { ...m, queuePosition: pos };
        }
        return m;
    });
    return touched ? next : matches;
}

// Bracket-variant of recomputeQueuePositions. Mirrors the Go-side
// annotateBracketQueuePositions in internal/mobileapp/handlers_match.go:
// per-court entries are sorted by (status priority, scheduledAt) before
// the counter is incremented, so positions match the order the viewer's
// ScheduleViewer actually renders rows in — including when a match's
// scheduledAt or court changed under SSE and storage order no longer
// reflects display order.
//
// FR-025: without this, an SSE patch transitioning a bracket match to
// completed would leave sibling bracket matches showing stale "N before
// yours" labels until the jittered fetchCompetitionDetails refresh lands
// (~500-1000ms later).
//
// Returns the original `bracket` reference when nothing needed to change,
// preserving React identity for memoised bracket components.
function recomputeBracketQueuePositions(bracket) {
    if (!bracket || !bracket.rounds || bracket.rounds.length === 0) return bracket;
    // Same older-payload guard as the pool helper: if no round has a
    // populated queuePosition, do nothing.
    const hasAnyQueuePosition = bracket.rounds.some(round =>
        round.some(m => m.queuePosition !== undefined)
    );
    if (!hasAnyQueuePosition) return bracket;

    // Flatten round/position pairs into entries the per-court sorter
    // can consume; idx encodes "round-position" for stable tie-break.
    const entries = [];
    bracket.rounds.forEach((round, ri) => {
        round.forEach((m, mi) => {
            entries.push({
                idx: ri * 100000 + mi, // safe room for any realistic round size
                m,
                court: m.court || "",
                ri,
                mi,
            });
        });
    });
    const byCourt = _orderByCourtKey(entries);
    const positionsByKey = new Map();
    for (const bucket of byCourt.values()) {
        let counter = 0;
        for (const e of bucket) {
            let pos = 0;
            if (e.m.status === "scheduled") {
                counter++;
                pos = counter;
            }
            positionsByKey.set(`${e.ri}:${e.mi}`, pos);
        }
    }

    let touched = false;
    const nextRounds = bracket.rounds.map((round, ri) => {
        let roundTouched = false;
        const nextRound = round.map((m, mi) => {
            const pos = positionsByKey.get(`${ri}:${mi}`) || 0;
            if ((m.queuePosition || 0) !== pos) {
                roundTouched = true;
                touched = true;
                return { ...m, queuePosition: pos };
            }
            return m;
        });
        return roundTouched ? nextRound : round;
    });
    return touched ? { ...bracket, rounds: nextRounds } : bracket;
}

// T099: re-broadcast competitor_status_updated SSE events as a window-level
// CustomEvent. Backend wire shape (per specs/003-tournament-gap-closure/
// contracts/match-decisions.md §SSE):
//   { type: "competitor_status_updated",
//     data: { competitionId, status: { playerId, eligible, reason,
//                                      matchId, recordedAt } } }
//
// applyPatch is the only SSE-entry point both app.jsx and admin.jsx route
// through, so handling status events here keeps the dispatch in one place
// without restructuring the surrounding subscribers. Subscribers (the
// schedule list, the score editor, the import panel) listen on
// `competitor-status-updated` and trigger a refetch — the simplest
// invalidator that doesn't require restructuring the prop-driven
// tournament state.
//
// We deliberately don't try to mutate `prev` for this event type: the
// ineligibility change affects derived match-list filtering (who's
// eligible for which match) rather than any single match's score, so a
// targeted in-place patch would have to re-walk both poolMatches and
// bracket.rounds plus seed/participant lists. A full refetch is cheaper
// to reason about and matches what the existing match_updated path does
// after applying the partial patch.
// T217 / A2 — SSE ordering gap detection. The backend stamps every
// envelope with a strictly-monotonic `seq` (T215) and retains the last
// N events for replay-on-reconnect (T216). The frontend tracks the
// highest seq seen and reacts on three conditions:
//
//   - seq === lastSeq + 1 → normal forward progress; advance and apply.
//   - seq <= lastSeq      → duplicate (typically a replayed event from
//                           a reconnect); drop silently to avoid
//                           re-applying a patch we've already merged.
//   - seq > lastSeq + 1   → gap detected (one or more events lost
//                           between the last live event and this one).
//                           Fire `onGap(missingRange)` so the caller
//                           can trigger a full refetch of the affected
//                           scope. We still apply the current patch
//                           since it's authoritative.
//
// The first event seen on a fresh subscription is always accepted (no
// `+ 1` check against the implicit zero) so initial load doesn't burn
// a false-positive gap on connect.
//
// `state` is an opaque object provided by the caller; we store and
// mutate `state.lastSeq` so multiple `applyPatchOrdered` calls share
// a single counter. Decoupling from a module-level singleton makes the
// helper safe to reuse across multiple competitions or test fixtures.
function applyPatchOrdered(prev, event, state, onGap) {
    if (!event || typeof event !== "object") return applyPatch(prev, event);
    const incoming = typeof event.seq === "number" ? event.seq : null;
    if (state && incoming != null) {
        const last = typeof state.lastSeq === "number" ? state.lastSeq : 0;
        if (last > 0 && incoming <= last) {
            // Duplicate / replay — drop silently. We've already
            // merged this seq's patch into `prev`; re-applying would
            // be harmless but wastes a render.
            return prev;
        }
        if (last > 0 && incoming > last + 1) {
            // Gap. Fire the callback with the missing range so the
            // caller can refetch. Still apply the current patch so
            // the user sees the latest known state immediately.
            if (typeof onGap === "function") {
                try {
                    onGap({ from: last + 1, to: incoming - 1 });
                } catch (err) {
                    // The console.error here is intentional: a thrown
                    // callback shouldn't break SSE processing.
                    console.error("SSE gap callback failed:", err);
                }
            }
        }
        state.lastSeq = incoming;
    }
    return applyPatch(prev, event);
}

function applyPatch(prev, event) {
    if (event && event.type === "competitor_status_updated" && event.data) {
        // Fire-and-forget; bail out early so the result/results plumbing
        // below doesn't reject the event for missing `result`.
        try {
            if (typeof window !== "undefined" && window.dispatchEvent) {
                window.dispatchEvent(new CustomEvent("competitor-status-updated", { detail: event.data }));
            }
        } catch (_) { /* ignore dispatch failures in non-DOM environments */ }
        // No tournament-state mutation here — caller re-fetches on the
        // same event via a window listener (see admin_schedule.jsx /
        // app.jsx subscribeToEvents). Return prev unchanged so memoised
        // children don't re-render gratuitously.
        return prev;
    }
    if (!prev || !event || !event.data) return prev;
    const { result, results } = event.data;
    const resultsToApply = results || (result ? [result] : []);
    if (resultsToApply.length === 0) return prev;

    const resultMap = new Map(resultsToApply.map(r => [r.id, r]));
    const next = { ...prev };
    let changed = false;
    // Track whether any pool patch changed a match's scheduled state;
    // only then is a queue-position recompute meaningful.
    let needsQueueRecompute = false;

    // Queue positions count *scheduled* matches only, so a recompute
    // is needed whenever a match leaves the scheduled state (→ running,
    // → completed, → cancelled / forfeit / kiken / etc.) OR when a
    // scheduled match's court/scheduledAt changes — the remaining
    // scheduled siblings on the same court need to shift up/down
    // immediately rather than waiting for the next jittered refresh.
    const isScheduleAffecting = (prevStatus, nextStatus, prevMatch, nextMatch) => {
        // Status transition off "scheduled" — any such transition
        // releases a slot in the per-court queue.
        if (prevStatus === "scheduled" && nextStatus !== "scheduled") return true;
        // Court or scheduledAt move while still scheduled — the
        // per-court bucket itself changes (or the within-court sort
        // order does), so siblings on either side need to re-rank.
        if (prevStatus === "scheduled" && nextStatus === "scheduled") {
            if ((prevMatch.court || "") !== (nextMatch.court || "")) return true;
            if ((prevMatch.scheduledAt || "") !== (nextMatch.scheduledAt || "")) return true;
        }
        return false;
    };

    if (next.poolMatches) {
        next.poolMatches = next.poolMatches.map(m => {
            const update = resultMap.get(m.id);
            if (update) {
                changed = true;
                const prevStatus = m.status;
                const prevCourt = m.court || "";
                const merged = _mergeMatchPatch(m, update);
                const statusFlipped = (prevStatus === "scheduled") !== (merged.status === "scheduled");
                // Court moves also stale per-court queue positions: a scheduled
                // match leaving court A and arriving on court B must re-number
                // both courts' remaining scheduled siblings.
                const courtMoved = (merged.court || "") !== prevCourt
                    && (prevStatus === "scheduled" || merged.status === "scheduled");
                if (statusFlipped || courtMoved) {
                    needsQueueRecompute = true;
                }
                return merged;
            }
            return m;
        });
        if (needsQueueRecompute) {
            next.poolMatches = recomputeQueuePositions(next.poolMatches);
        }
    }

    if (next.bracket && next.bracket.rounds) {
        let bChanged = false;
        // Track whether any bracket patch was a schedule-affecting
        // transition; same trigger semantics as the pool branch above.
        let bracketNeedsQueueRecompute = false;
        const rounds = next.bracket.rounds.map(round =>
            round.map(m => {
                const update = resultMap.get(m.id);
                if (update) {
                    bChanged = true; changed = true;
                    // Map MatchResult to BracketMatch fields. Bracket
                    // matches keep ippons as joined strings (scoreA/scoreB),
                    // not arrays — without this the next render would see
                    // scoreA undefined and the bracket cell would go blank.
                    const patch = { ...update };
                    if (patch.ipponsA) patch.scoreA = patch.ipponsA.join("");
                    if (patch.ipponsB) patch.scoreB = patch.ipponsB.join("");
                    const prevStatus = m.status;
                    const prevCourt = m.court || "";
                    const merged = _mergeMatchPatch(m, patch);
                    const statusFlipped = (prevStatus === "scheduled") !== (merged.status === "scheduled");
                    // Same court-move invalidation as the pool branch above.
                    const courtMoved = (merged.court || "") !== prevCourt
                        && (prevStatus === "scheduled" || merged.status === "scheduled");
                    if (statusFlipped || courtMoved) {
                        bracketNeedsQueueRecompute = true;
                    }
                    return merged;
                }
                return m;
            })
        );
        if (bChanged) {
            next.bracket = { ...next.bracket, rounds };
            if (bracketNeedsQueueRecompute) {
                next.bracket = recomputeBracketQueuePositions(next.bracket);
            }
        }
    }

    return changed ? next : prev;
}

export { applyPatch, applyPatchOrdered, recomputeQueuePositions, recomputeBracketQueuePositions };
