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
// Queue positions (FR-025): a patch that moves a match into or out of a
// court queue renumbers the competition's own matches on that court before
// applyPatch returns (shiftQueuePositions, below).

// Direct ESM import of the real merge helper instead of the previous
// window.mergeMatchPatch / spread-fallback dance. The spread fallback
// silently dropped court/scheduledAt preservation when data.jsx hadn't
// loaded yet (a test-time hazard, since production index.html ordering
// always loaded data.js first). The explicit import makes the
// dependency a build-graph contract: esbuild will fail loudly rather
// than fall through to a wrong-shape merge.
import { mergeMatchPatch as _mergeMatchPatch, olderByScoreVersion } from './data.jsx';
import { normalizeMatch, buildPlayerMap } from './api_serializers.jsx';
import { isBarredMatch } from './ineligible_match.jsx';
import { compareCourtQueue, courtQueueEntriesOf, withCourtQueuePositions } from './court_queue.jsx';

// bc-cse: _mergeMatchPatchKeepIneligible, which used to live here, is gone.
// It defended `ineligibleSides` against an explicit `null` in the patch, but
// the field is a Go pointer with `omitempty` (internal/state/models.go), so
// an absent stamp is OMITTED from the wire, never sent as `null` or `{}` --
// that shape cannot occur. An omitted key already survives the ordinary
// `{...existing, ...patch}` spread `_mergeMatchPatch` performs unchanged, so
// the wrapper was defending against a payload the server can never send.

// Positions are the server's court-wide numbers (court_queue.jsx), counted
// across every competition on the court. A patch holds ONE competition, so it
// keeps the server's numbers and moves only this competition's own matches on
// the court the patched match leaves or enters, never giving two of them the
// same number:
//   - a match that LEAVES the queue (no longer scheduled, or now barred) reads
//     0, and each of its siblings on that court numbered after it moves down one;
//   - a match that ENTERS it (now scheduled and unbarred) takes the place after
//     the last sibling on its court that sorts before it by the court-queue
//     order (compareCourtQueue), else the place of the first sibling that sorts
//     after it, and the siblings from that place move up one; with no numbered
//     sibling on the court it takes 1 unless a queued sibling sorts before it
//     (only scheduled, unbarred matches count, as on the server), else it stays
//     0 for the refetch;
//   - a court or time move of a match still queued is a leave, then an enter.
// Another competition's matches on the court keep their numbers, so the host's
// refetch stays the source of truth; a holder of every competition re-stamps
// the court-wide numbers instead (applyPatchToCompetitions).
const isQueued = (m) => !!m && m.status === 'scheduled' && !isBarredMatch(m);

function shiftQueuePositions(comp, changes) {
    const entries = courtQueueEntriesOf(comp);
    if (entries.length === 0) return comp;
    const entryOf = new Map(entries.map((e) => [e.match, e]));
    const pos = new Map(entries.map((e) => [e, e.match.queuePosition || 0]));
    // The court each number is held on: a changed match holds its old one on
    // its old court until it leaves.
    const heldOn = new Map(entries.map((e) => [e, e.court]));
    const changed = [];
    for (const { before, after } of changes) {
        const e = entryOf.get(after);
        if (!e) continue;
        pos.set(e, isQueued(before) ? (before.queuePosition || 0) : 0);
        heldOn.set(e, before.court || "");
        changed.push(e);
    }
    // Every change leaves first, so none still to enter holds a stale number.
    for (const e of changed) {
        const p = pos.get(e);
        const court = heldOn.get(e);
        pos.set(e, 0);
        heldOn.set(e, e.court);
        if (p === 0) continue;
        for (const [o, q] of pos) {
            if (heldOn.get(o) === court && q > p) pos.set(o, q - 1);
        }
    }
    const entering = changed.filter((e) => isQueued(e.match)).sort(compareCourtQueue);
    for (const e of entering) {
        const placed = entries.filter((o) => o !== e && pos.get(o) > 0 && heldOn.get(o) === e.court);
        const earlier = placed.filter((o) => compareCourtQueue(o, e) < 0);
        let p = 0;
        if (earlier.length > 0) {
            p = Math.max(...earlier.map((o) => pos.get(o))) + 1;
        } else if (placed.length > 0) {
            p = Math.min(...placed.map((o) => pos.get(o)));
        } else if (!entries.some((o) => o !== e && o.court === e.court && isQueued(o.match) && compareCourtQueue(o, e) < 0)) {
            p = 1;
        }
        if (p === 0) continue;
        for (const o of placed) {
            if (pos.get(o) >= p) pos.set(o, pos.get(o) + 1);
        }
        pos.set(e, p);
    }

    // Identity is kept wherever a number does not change (memoised children).
    const stamp = (m) => {
        const e = entryOf.get(m);
        const p = e ? pos.get(e) : (m.queuePosition || 0);
        return (m.queuePosition || 0) === p ? m : { ...m, queuePosition: p };
    };
    const mapKeep = (arr, fn) => {
        const next = arr.map(fn);
        return next.some((x, i) => x !== arr[i]) ? next : arr;
    };
    const poolMatches = comp.poolMatches ? mapKeep(comp.poolMatches, stamp) : comp.poolMatches;
    let bracket = comp.bracket;
    if (bracket) {
        const rounds = bracket.rounds ? mapKeep(bracket.rounds, (round) => mapKeep(round, stamp)) : bracket.rounds;
        const bronze = bracket.thirdPlaceMatch ? stamp(bracket.thirdPlaceMatch) : bracket.thirdPlaceMatch;
        if (rounds !== bracket.rounds || bronze !== bracket.thirdPlaceMatch) {
            bracket = { ...bracket, rounds, thirdPlaceMatch: bronze };
        }
    }
    if (poolMatches === comp.poolMatches && bracket === comp.bracket) return comp;
    return { ...comp, poolMatches, bracket };
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
    return patchCompetition(prev, event).next;
}

// applyPatchToCompetitions patches one competition of a list that holds EVERY
// competition the court-wide numbers count (the tournament's, a display's
// tree), and re-stamps those numbers through withCourtQueuePositions when the
// patch moved a match into or out of a queue. Returns the list unchanged when
// nothing in it changed.
function applyPatchToCompetitions(competitions, compId, event, idOf = (c) => c && c.id) {
    if (!Array.isArray(competitions)) return competitions;
    let changed = false;
    let queueMoved = false;
    const next = competitions.map((c) => {
        if (idOf(c) !== compId) return c;
        const res = patchCompetition(c, event);
        if (res.next !== c) changed = true;
        if (res.queueMoved) queueMoved = true;
        return res.next;
    });
    if (!changed) return competitions;
    return queueMoved ? withCourtQueuePositions(next) : next;
}

// patchCompetition is applyPatch's body: the patched competition, and whether
// the patch moved a match into or out of a court queue.
function patchCompetition(prev, event) {
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
        return { next: prev, queueMoved: false };
    }
    const unchanged = { next: prev, queueMoved: false };
    if (!prev || !event || !event.data) return unchanged;
    const { result, results } = event.data;
    const resultsToApply = results || (result ? [result] : []);
    if (resultsToApply.length === 0) return unchanged;

    // Skip null/primitive/id-less entries before building the lookup: a
    // malformed event (e.g. results:[null], whether from a rogue same-origin
    // BroadcastChannel patch or a bad server payload) would otherwise throw on
    // r.id inside the Map constructor. Such entries can never match a real
    // match id anyway, so dropping them is behaviour-preserving for valid input.
    const resultMap = new Map(
        resultsToApply.filter(r => r && r.id != null).map(r => [r.id, r]),
    );
    let next = { ...prev };
    let changed = false;
    // The patched matches that joined or left a court queue, or moved within
    // one, as {before, after}: shiftQueuePositions renumbers their siblings.
    const queueChanges = [];

    // Built lazily: most events won't hit any of our match IDs (e.g.
    // sibling-competition updates) so we skip the O(participants) scan
    // until we actually find a match that needs normalization (T093).
    let playerMap;
    const getPlayerMap = () => playerMap ?? (playerMap = buildPlayerMap(prev));

    // A match is in a court queue while it is scheduled and unbarred, so its
    // place changes when it joins or leaves (a start, a finish, a send back
    // to the queue, a reopen, a bar recorded or lifted: a patch that carries
    // ineligibleSides can flip it), and when a queued match moves court or
    // time, which re-ranks it among its siblings.
    const isScheduleAffecting = (prevMatch, nextMatch) => {
        if (isQueued(prevMatch) !== isQueued(nextMatch)) return true;
        if (isQueued(prevMatch)) {
            if ((prevMatch.court || "") !== (nextMatch.court || "")) return true;
            if ((prevMatch.scheduledAt || "") !== (nextMatch.scheduledAt || "")) return true;
        }
        return false;
    };
    const patchMatch = (m) => {
        const update = resultMap.get(m.id);
        if (!update) return m;
        changed = true;
        const merged = normalizeMatch(_mergeMatchPatch(m, update), getPlayerMap());
        if (isScheduleAffecting(m, merged)) queueChanges.push({ before: m, after: merged });
        return merged;
    };

    if (next.poolMatches) {
        const pool = next.poolMatches.map(patchMatch);
        if (pool.some((x, i) => x !== next.poolMatches[i])) next.poolMatches = pool;
    }

    if (next.bracket && next.bracket.rounds) {
        let bChanged = false;
        const rounds = next.bracket.rounds.map(round =>
            round.map(m => {
                const merged = patchMatch(m);
                if (merged !== m) bChanged = true;
                return merged;
            })
        );
        if (bChanged) {
            next.bracket = { ...next.bracket, rounds };
        }

        // The single-3rd bronze match (thirdPlaceMatch) is a SIBLING of
        // bracket.rounds, not a row inside it, so the loop above never sees it.
        // Apply an SSE match_updated for the bronze here (same field-mapping as
        // a round match), else its score stays stale until the background
        // refetch.
        const bm = next.bracket.thirdPlaceMatch;
        if (bm) {
            const merged = patchMatch(bm);
            if (merged !== bm) next.bracket = { ...next.bracket, thirdPlaceMatch: merged };
        }
    }

    if (!changed) return unchanged;
    // Pool and knockout matches share a court's queue, so they are renumbered
    // together.
    if (queueChanges.length > 0) next = shiftQueuePositions(next, queueChanges);
    return { next, queueMoved: queueChanges.length > 0 };
}

// isOlderRunningCopy: whether `fetched` is a running match older than a
// running copy held at `heldStamp` (and `heldVersion`). The one rule
// keepNewerMatches and a score editor's held answer both apply; see
// keepNewerMatches for why only running over running. bc-evts S5: when both
// copies carry the server's scoreVersion, it decides (olderByScoreVersion,
// data.jsx): it reads no device's clock. Otherwise only a stamped fetched copy
// older than the held stamp is older.
function isOlderRunningCopy(fetched, heldStatus, heldStamp, heldVersion) {
    if (heldStatus !== "running" || fetched.status !== "running") return false;
    const byVersion = olderByScoreVersion(fetched, { scoreVersion: heldVersion });
    if (byVersion !== null) return byVersion;
    return fetched.modifiedAt > 0 && (heldStamp || 0) > fetched.modifiedAt;
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
        return h && isOlderRunningCopy(row, h.status, h.modifiedAt, h.scoreVersion) ? h : row;
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

// matchInComp: a competition's match by id, a pool match, a knockout round's
// or the 3rd-place match, or null. Shared by the court console and the admin
// host, which reads a match's court or time before changing it (R4, `was`).
function matchInComp(comp, id) {
    if (!comp) return null;
    const pool = (comp.poolMatches || []).find((m) => m.id === id);
    if (pool) return pool;
    const b = comp.bracket;
    if (!b) return null;
    for (const round of b.rounds || []) {
        const m = round.find((x) => x.id === id);
        if (m) return m;
    }
    return b.thirdPlaceMatch && b.thirdPlaceMatch.id === id ? b.thirdPlaceMatch : null;
}

export {
    applyPatch, applyPatchToCompetitions, applyPatchOrdered, checkSeqGap,
    keepNewerMatches, keepNewerDetail, keepNewerCompetitions, keepNewerTournament, isOlderRunningCopy,
    matchInComp,
};
