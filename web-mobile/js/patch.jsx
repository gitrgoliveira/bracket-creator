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

function applyPatch(prev, event) {
    if (!prev || !event || !event.data) return prev;
    const { result, results } = event.data;
    const resultsToApply = results || (result ? [result] : []);
    if (resultsToApply.length === 0) return prev;

    const resultMap = new Map(resultsToApply.map(r => [r.id, r]));
    const next = { ...prev };
    let changed = false;
    // Track whether any patch was a scheduled/running → completed
    // transition; only then is a queue-position recompute meaningful.
    let needsQueueRecompute = false;

    if (next.poolMatches) {
        next.poolMatches = next.poolMatches.map(m => {
            const update = resultMap.get(m.id);
            if (update) {
                changed = true;
                const prevStatus = m.status;
                const merged = _mergeMatchPatch(m, update);
                if (merged.status === "completed" && (prevStatus === "scheduled" || prevStatus === "running")) {
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
                    return _mergeMatchPatch(m, patch);
                }
                return m;
            })
        );
        if (bChanged) next.bracket = { ...next.bracket, rounds };
    }

    return changed ? next : prev;
}

export { applyPatch, recomputeQueuePositions };
