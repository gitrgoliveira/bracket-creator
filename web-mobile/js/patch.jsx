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

// Recompute queuePosition on scheduled poolMatches per court after an
// SSE patch transitions a match to completed. The backend recomputes
// server-side on the next GET (see internal/state/match.go), but SSE
// patches only carry the single updated match — without this client-side
// step the UI would still show stale "3 before yours" labels until the
// next viewer refresh. FR-025, R3.
//
// Algorithm mirrors state.DeriveQueuePositions: walk the slice in its
// current order and assign 1, 2, 3, … to scheduled matches per court.
// Running/completed get 0. The slice is already ordered by the server
// (typically by scheduledAt), so re-walking in array order produces the
// same positions the server would on its next response.
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
    const counters = {};
    let touched = false;
    const next = matches.map(m => {
        let pos = 0;
        if (m.status === "scheduled") {
            const court = m.court || "";
            counters[court] = (counters[court] || 0) + 1;
            pos = counters[court];
        }
        if ((m.queuePosition || 0) !== pos) {
            touched = true;
            return { ...m, queuePosition: pos };
        }
        return m;
    });
    return touched ? next : matches;
}

// Bracket-variant of recomputeQueuePositions. Walks bracket.rounds in
// round-then-position order (mirrors the Go-side annotateBracketQueuePositions
// in internal/mobileapp/handlers_match.go) and assigns 1-indexed positions to
// scheduled matches per court. Running/completed get 0.
//
// FR-025: without this, an SSE patch transitioning a bracket match to
// completed would leave sibling bracket matches showing stale "N before
// yours" labels until the jittered fetchCompetitionDetails refresh lands
// (~500-1000ms later). Mirrors the pool helper exactly — the only
// structural difference is the nested round-then-position iteration.
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
    const counters = {};
    let touched = false;
    const nextRounds = bracket.rounds.map(round => {
        let roundTouched = false;
        const nextRound = round.map(m => {
            let pos = 0;
            if (m.status === "scheduled") {
                const court = m.court || "";
                counters[court] = (counters[court] || 0) + 1;
                pos = counters[court];
            }
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
        // Track whether any bracket patch changed a match's scheduled state;
        // same trigger as the pool branch above.
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
