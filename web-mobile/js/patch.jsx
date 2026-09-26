// SSE patch-apply logic, centralised.
//
// app.jsx and admin.jsx both used to carry an identical
// patchCompetitionData(prev, event) implementation that walked the
// competition's poolMatches and bracket.rounds, looked up each match by
// id in the SSE event payload, and merged via mergeMatchPatch (declared
// in data.jsx). This module is the single source of truth for that
// logic: both app.jsx and admin.jsx now import it.
//
// The match-update SSE payload is shaped as either:
//   { result: { id, ipponsA, ipponsB, hansokuA, hansokuB, ... } }
//   { results: [ { id, ... }, ... ] }
// We accept both forms so a future server bulk-update doesn't require
// a frontend release. results[] takes precedence over result if both
// are present.
//
// Pool and bracket matches share one wire shape (ipponsA/ipponsB arrays;
// scoreA/scoreB strings never appear), so both branches below hand the
// patch straight to mergeMatchPatch with no per-kind translation.
//
// Returns the unchanged `prev` reference if no listed result IDs
// matched any match in the competition. This avoids unnecessary
// re-renders downstream when a competition receives an SSE event for
// a sibling competition's match (the caller is expected to pre-filter
// by competitionId, but defensive identity-preservation here is cheap).
//
// FR-025 hook (queue-position recompute): listeners that need to react
// to status transitions (running/scheduled → completed) can compose with
// applyPatch by reading the merged result and recomputing derived
// fields. Slice 1's T049 will add a queue-position invalidator that
// runs on the same event before applyPatch returns.

// Direct ESM import of the real merge helper instead of the previous
// window.mergeMatchPatch / spread-fallback dance. The spread fallback
// silently dropped court/scheduledAt preservation when data.jsx hadn't
// loaded yet (a test-time hazard, since production index.html ordering
// always loaded data.js first). The explicit import makes the
// dependency a build-graph contract: esbuild will fail loudly rather
// than fall through to a wrong-shape merge.
import { mergeMatchPatch as _mergeMatchPatch } from './data.jsx';
import { normalizeMatch, buildPlayerMap } from './api_serializers.jsx';
import { isBarredMatch } from './ineligible_match.jsx';

// bc-cse: _mergeMatchPatchKeepIneligible, which used to live here, is gone.
// It defended `ineligibleSides` against an explicit `null` in the patch, but
// the field is a Go pointer with `omitempty` (internal/state/models.go), so
// an absent stamp is OMITTED from the wire, never sent as `null` or `{}` --
// that shape cannot occur. An omitted key already survives the ordinary
// `{...existing, ...patch}` spread `_mergeMatchPatch` performs unchanged, so
// the wrapper was defending against a payload the server can never send.

// statusSortOrder mirrors annotateBracketQueuePositions in
// internal/mobileapp/handlers_match.go and the per-court sort in
// ScheduleViewer (viewer.jsx). Running ranks ahead of scheduled, which
// ranks ahead of completed/everything-else.
const statusSortOrder = (s) => (s === "running" ? 0 : s === "scheduled" ? 1 : 2);

// Stable per-court ordering: gather pointers to the original entries,
// sort them by (status priority, scheduledAt, original index) without
// mutating the input array. Returns a Map of court → sorted entry
// arrays; caller iterates per-court and increments a counter to derive
// queue positions.
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

// Recompute queuePosition on poolMatches per court after any SSE patch
// that could invalidate the queue. The backend recomputes server-side
// on the next GET (see internal/state/match.go), but SSE patches only
// carry the single updated match: without this client-side step the
// UI would show stale "N before yours" labels until the next viewer
// refresh. FR-025, R3.
//
// Triggered by applyPatch's `isScheduleAffecting` on any change that
// affects the per-court queue:
//   - status transition into or out of `scheduled` (completed, running,
//     forfeit, kiken, and the admin-correction reverse direction)
//   - court move while still scheduled
//   - scheduledAt move while still scheduled
//
// Algorithm mirrors annotateQueuePositions in handlers_match.go (which
// delegates to state.DeriveQueuePositions): gather per-court entries,
// sort by (status priority, scheduledAt, original index) so the counter
// increments in viewer-display order, assign 1, 2, 3, … to scheduled
// matches and 0 to everything else.
//
// Touches only matches whose existing queuePosition differs from the
// recomputed value, preserving object identity for unaffected matches
// (matters for React.memo on VSchedItem / TWMatch).
function recomputeQueuePositions(matches) {
    if (!matches || matches.length === 0) return matches;

    // Build per-court ordered buckets and derive positions unconditionally:
    // even when no matches are scheduled: because we also need to
    // clear stale non-zero queuePosition values on matches that just
    // transitioned off `scheduled` (the last scheduled match becoming
    // running/completed must drop its "Next up" label to 0).
    // _mergeMatchPatch preserves fields not in the patch, so a stale
    // qp would linger on the transitioned match until the next GET
    // refresh otherwise. The touched-tracking below still preserves
    // identity when nothing actually changes (e.g. all qps already 0).
    const entries = matches.map((m, idx) => ({ idx, m, court: m.court || "" }));
    const byCourt = _orderByCourtKey(entries);
    const newPositions = Array.from({ length: matches.length }, () => 0);
    for (const bucket of byCourt.values()) {
        let counter = 0;
        for (const e of bucket) {
            // A barred match (ineligible_match.jsx) cannot be fought as
            // scheduled: it holds position 0 and is skipped by the counter,
            // so later matches move up to take the slot it would have held.
            if (e.m.status === "scheduled" && !isBarredMatch(e.m)) {
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
// ScheduleViewer actually renders rows in: including when a match's
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
    // Run unconditionally: even when no bracket match is currently
    // scheduled: because we also need to clear stale non-zero
    // queuePosition values on matches that just transitioned off
    // `scheduled` (last scheduled bracket match completing must drop
    // its qp to 0). _mergeMatchPatch preserves fields not in the patch,
    // so a stale qp would linger otherwise. The per-round touched-
    // tracking below still preserves identity when nothing changes.

    // Flatten round/position pairs into entries the per-court sorter
    // can consume. `idx` is a monotonic push-order counter so the
    // (round, position) traversal order doubles as a stable tie-break
    // for _orderByCourtKey when status + scheduledAt are equal. No
    // magic number, no upper-bound assumption on round size.
    const entries = [];
    bracket.rounds.forEach((round, ri) => {
        round.forEach((m, mi) => {
            entries.push({
                idx: entries.length,
                m,
                court: m.court || "",
                ri,
                mi,
            });
        });
    });
    // The single-3rd bronze match (thirdPlaceMatch) is a SIBLING of bracket.rounds,
    // not a row in it, but it shares a court with the final and must take part in
    // that court's queue ordering. The bronze is conventionally played JUST
    // BEFORE the final (viewer_awards.jsx: "the bronze is normally played
    // first"). _orderByCourtKey tie-breaks on idx when status+scheduledAt are
    // equal, and the final is the last-pushed entry (last round's sole match),
    // so give the bronze idx = finalIdx - 0.5 to slot it immediately before the
    // final and after the semifinals. Key it "bronze:0" so it can't collide with
    // any real "ri:mi".
    if (bracket.thirdPlaceMatch) {
        const finalIdx = entries.length - 1;
        entries.push({
            idx: finalIdx - 0.5,
            m: bracket.thirdPlaceMatch,
            court: bracket.thirdPlaceMatch.court || "",
            ri: "bronze",
            mi: 0,
        });
    }
    const byCourt = _orderByCourtKey(entries);
    const positionsByKey = new Map();
    for (const bucket of byCourt.values()) {
        let counter = 0;
        for (const e of bucket) {
            let pos = 0;
            // Same barred-skip rule as recomputeQueuePositions above.
            if (e.m.status === "scheduled" && !isBarredMatch(e.m)) {
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
    // Write the bronze's recomputed queue position back (keyed "bronze:0").
    let nextBronze = bracket.thirdPlaceMatch;
    if (bracket.thirdPlaceMatch) {
        const pos = positionsByKey.get("bronze:0") || 0;
        if ((bracket.thirdPlaceMatch.queuePosition || 0) !== pos) {
            nextBronze = { ...bracket.thirdPlaceMatch, queuePosition: pos };
            touched = true;
        }
    }
    if (!touched) return bracket;
    const result = { ...bracket, rounds: nextRounds };
    if (nextBronze !== bracket.thirdPlaceMatch) result.thirdPlaceMatch = nextBronze;
    return result;
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
// `competitor-status-updated` and trigger a refetch: the simplest
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
// T217 / A2: SSE ordering gap detection. The backend stamps every
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

// checkSeqGap: pure seq bookkeeping extracted from applyPatchOrdered.
// Handles only the monotonic-seq tracking without touching the patch
// payload. Consumers MUST call this on every SSE event that carries a
// numeric `seq` so the counter advances accurately: otherwise an
// untracked seq'd event makes the next patched event falsely look like a
// gap. It is safe (and a no-op) to call on events WITHOUT a numeric seq:
// heartbeats and other non-seq frames early-return without mutating state,
// so callers can pass every event through unconditionally rather than
// special-casing them out (and risk filtering a real event by mistake).
// Heartbeats do NOT advance the seq counter.
//
// Returns:
//   { duplicate: false, gap: false }: normal forward or first event
//   { duplicate: true, gap: false }: seq <= lastSeq (replay/dup)
//   { gap: true, duplicate: false }: seq jumped, onGap fired
//
// `seq` not a number → treat as no-seq event; state is not mutated.
// `state.lastSeq === 0` (or absent) → first event; accepted, no gap check.
function checkSeqGap(state, seq, onGap) {
    if (typeof seq !== "number") return { duplicate: false, gap: false };
    // Null-safe: without a state object there's nothing to track against and the
    // `state.lastSeq = seq` writes below would throw. Callers always pass one;
    // this guards misuse / a future caller that doesn't.
    if (!state) return { duplicate: false, gap: false };
    const last = (typeof state.lastSeq === "number") ? state.lastSeq : 0;
    if (last > 0 && seq <= last) {
        // Duplicate / replay: do NOT advance lastSeq.
        return { duplicate: true, gap: false };
    }
    if (last > 0 && seq > last + 1) {
        // Gap. Fire the callback with the missing range.
        if (typeof onGap === "function") {
            try {
                onGap({ from: last + 1, to: seq - 1 });
            } catch (err) {
                // Intentional: a thrown callback shouldn't break SSE processing.
                console.error("SSE gap callback failed:", err);
            }
        }
        state.lastSeq = seq;
        return { gap: true, duplicate: false };
    }
    // Normal forward progress (or first event).
    state.lastSeq = seq;
    return { gap: false, duplicate: false };
}

function applyPatchOrdered(prev, event, state, onGap) {
    if (!event || typeof event !== "object") return applyPatch(prev, event);
    const incoming = typeof event.seq === "number" ? event.seq : null;
    if (state && incoming != null) {
        const result = checkSeqGap(state, incoming, onGap);
        if (result.duplicate) {
            // Duplicate / replay: drop silently. We've already
            // merged this seq's patch into `prev`; re-applying would
            // be harmless but wastes a render.
            return prev;
        }
        // gap:true or gap:false (normal): fall through to applyPatch.
        // checkSeqGap already advanced state.lastSeq and fired onGap.
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
        // No tournament-state mutation here: caller re-fetches on the
        // same event via a window listener (see admin_schedule.jsx /
        // app.jsx subscribeToEvents). Return prev unchanged so memoised
        // children don't re-render gratuitously.
        return prev;
    }
    if (!prev || !event || !event.data) return prev;
    const { result, results } = event.data;
    const resultsToApply = results || (result ? [result] : []);
    if (resultsToApply.length === 0) return prev;

    // Skip null/primitive/id-less entries before building the lookup: a
    // malformed event (e.g. results:[null], whether from a rogue same-origin
    // BroadcastChannel patch or a bad server payload) would otherwise throw on
    // r.id inside the Map constructor. Such entries can never match a real
    // match id anyway, so dropping them is behaviour-preserving for valid input.
    const resultMap = new Map(
        resultsToApply.filter(r => r && r.id != null).map(r => [r.id, r]),
    );
    const next = { ...prev };
    let changed = false;
    // Track whether any pool patch changed a match's scheduled state;
    // only then is a queue-position recompute meaningful.
    let needsQueueRecompute = false;

    // Built lazily: most events won't hit any of our match IDs (e.g.
    // sibling-competition updates) so we skip the O(participants) scan
    // until we actually find a match that needs normalization (T093).
    let playerMap;
    const getPlayerMap = () => playerMap ?? (playerMap = buildPlayerMap(prev));

    // Queue positions count *scheduled* matches only, so a recompute
    // is needed whenever a match's "scheduled-ness" changes: leaving
    // (→ running / completed / cancelled / forfeit / kiken / …) OR
    // entering (admin correction reverts a completed match back to
    // scheduled): and whenever a still-scheduled match's
    // court/scheduledAt moves so siblings on either side re-rank.
    const isScheduleAffecting = (prevStatus, nextStatus, prevMatch, nextMatch) => {
        // Any change in scheduled-ness flips the per-court queue's
        // membership: either releasing a slot (leaving scheduled) or
        // claiming one (entering scheduled). Both directions matter.
        if ((prevStatus === "scheduled") !== (nextStatus === "scheduled")) return true;
        // Court or scheduledAt move while still scheduled: the
        // per-court bucket itself changes (or the within-court sort
        // order does), so siblings on either side need to re-rank.
        if (prevStatus === "scheduled" && nextStatus === "scheduled") {
            if ((prevMatch.court || "") !== (nextMatch.court || "")) return true;
            if ((prevMatch.scheduledAt || "") !== (nextMatch.scheduledAt || "")) return true;
            // A patch that DOES carry ineligibleSides (the contract only says
            // one "may" arrive without it) can flip whether this still-
            // scheduled match is barred. recomputeQueuePositions gives a
            // barred match position 0 and skips it in the per-court count, so
            // that flip changes queue membership exactly like leaving/entering
            // `scheduled` does: siblings on either side must re-rank.
            if (isBarredMatch(prevMatch) !== isBarredMatch(nextMatch)) return true;
        }
        return false;
    };

    if (next.poolMatches) {
        next.poolMatches = next.poolMatches.map(m => {
            const update = resultMap.get(m.id);
            if (update) {
                changed = true;
                const merged = normalizeMatch(_mergeMatchPatch(m, update), getPlayerMap());
                if (isScheduleAffecting(m.status, merged.status, m, merged)) {
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
                    const merged = normalizeMatch(_mergeMatchPatch(m, update), getPlayerMap());
                    if (isScheduleAffecting(m.status, merged.status, m, merged)) {
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

        // The single-3rd bronze match (thirdPlaceMatch) is a SIBLING of
        // bracket.rounds, not a row inside it, so the loop above never sees it.
        // Apply an SSE match_updated for the bronze here (same field-mapping as
        // a round match), else its score stays stale until the background
        // refetch. Recompute queue positions when its status transitions.
        if (next.bracket.thirdPlaceMatch) {
            const bm = next.bracket.thirdPlaceMatch;
            const update = resultMap.get(bm.id);
            if (update) {
                changed = true;
                const merged = normalizeMatch(_mergeMatchPatch(bm, update), getPlayerMap());
                next.bracket = { ...next.bracket, thirdPlaceMatch: merged };
                if (isScheduleAffecting(bm.status, merged.status, bm, merged)) {
                    next.bracket = recomputeBracketQueuePositions(next.bracket);
                }
            }
        }
    }

    return changed ? next : prev;
}

// keepNewerMatches: the competition a refetch answered with, keeping a running
// match the caller already holds from a NEWER write -- a live score never goes
// back in time under a score editor. A holder applies a push at once
// (applyPatch) and refetches a moment later, and two refetches can be in
// flight together, so an answer that read the data just before a write
// committed can land after that write is shown. Taken whole, it put the older
// scoreline back, and an open score editor that had caught up adopted it, so
// its next save wrote the lost point away. Only the holders a score editor
// reads from use it; a display takes a refetch whole. `held` and
// `fetched` are the SAME competition (match ids repeat across competitions);
// no held copy yet means nothing to keep.
function keepNewerMatches(held, fetched) {
    if (!held) return fetched;
    const heldById = new Map();
    for (const m of held.poolMatches || []) heldById.set(m.id, m);
    const hb = held.bracket;
    for (const round of (hb && hb.rounds) || []) for (const m of round) heldById.set(m.id, m);
    if (hb && hb.thirdPlaceMatch) heldById.set(hb.thirdPlaceMatch.id, hb.thirdPlaceMatch);
    // Only a RUNNING match is kept, and only over a RUNNING fetched copy with
    // an older stamp: that is the whole harm (a live score going back under an
    // open editor). Stamps come from two clocks: a client write carries the
    // device's estimate of server time, which the server accepts up to 5s
    // ahead, while a reopen, requeue or send-back is stamped by the server's
    // own clock, so a newer server stamp can read older than the write before
    // it. Every server-stamped change moves the status, so comparing only
    // running with running never weighs one clock against the other. An
    // unstamped fetched copy (a draw discarded and drawn again, reusing the
    // match ids) always replaces too.
    const newer = (row) => {
        const h = heldById.get(row.id);
        return h && h.status === "running" && row.status === "running" && row.modifiedAt > 0 && (h.modifiedAt || 0) > row.modifiedAt ? h : row;
    };
    const b = fetched.bracket;
    return {
        ...fetched,
        poolMatches: fetched.poolMatches && fetched.poolMatches.map(newer),
        bracket: b && {
            ...b,
            rounds: b.rounds && b.rounds.map((round) => round.map(newer)),
            thirdPlaceMatch: b.thirdPlaceMatch && newer(b.thirdPlaceMatch),
        },
    };
}

// keepNewerDetail is keepNewerMatches for a competition-detail response
// ({config, poolMatches, bracket, ...}): the held copy counts only when it is
// the same competition, so a refetch after navigating elsewhere is taken whole.
function keepNewerDetail(held, fetched) {
    return held && held.config.id === fetched.config.id ? keepNewerMatches(held, fetched) : fetched;
}

// keepNewerCompetitions is keepNewerMatches over a list of competitions (the
// court console's feed, the tournament's competitions), paired by id. No held
// list yet means nothing to keep.
function keepNewerCompetitions(held, fetched) {
    if (!Array.isArray(held)) return fetched;
    return fetched.map((comp) => keepNewerMatches(held.find((c) => c.id === comp.id), comp));
}

// keepNewerTournament applies it to the tournament's competitions. Its
// refreshes come from two sources that can be in flight together (the one
// after each save and the one after each server event), so an answer that
// read the data earlier can land later.
function keepNewerTournament(held, fetched) {
    return held ? { ...fetched, competitions: keepNewerCompetitions(held.competitions, fetched.competitions) } : fetched;
}

export {
    applyPatch, applyPatchOrdered, checkSeqGap, recomputeQueuePositions, recomputeBracketQueuePositions,
    keepNewerMatches, keepNewerDetail, keepNewerCompetitions, keepNewerTournament,
};
