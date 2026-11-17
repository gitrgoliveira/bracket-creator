// The court console's feed: keeping the console's copy of the court current.
// The status moves this device made that the feed does not show yet, local
// bracket propagation, and the reconnect refetcher. Moved out of
// admin_shiaijo.jsx; a leaf of the admin_shiaijo family (it imports none of its
// siblings).

import { sideKeyForDecisionBy } from './ineligible_match.jsx';

export const matchKey = (m) => `${m.compId}:${m.id}`;

export const readOwnMove = (m) => (window.API && typeof window.API.ownStatusMove === "function"
    ? window.API.ownStatusMove(m.compId, m.id) : null);

// The match id and the court a match event names, read off either the event's
// own fields or its `result` row; court is null when it names none.
export const eventMatchCourt = (d) => {
    const res = d && d.result;
    const court = d && typeof d.court === "string" ? d.court : (res && typeof res.court === "string" ? res.court : null);
    const mid = d && (d.matchId != null ? d.matchId : (res && res.id));
    return { court, mid };
};

// bracketSideName reads a bracket side that may be a normalized {id,name}
// object or a bare string. Mirrors admin_helpers.sideName without importing it,
// so this module stays pure for testing; admin_shiaijo imports it from here.
export function bracketSideName(side) {
    if (!side) return "";
    return typeof side === "object" ? (side.name || "") : side;
}

// ownStatusMoveHolds: whether the console still shows match `m` with the
// status move this device made (`mv`, from the outbox's ONE owner,
// API.ownStatusMove: a send back, a finish or a decision; stage 6 review 8,
// M4) over the feed's copy. Not once the feed shows that status or no longer
// holds the match. While the move is queued it holds: the copy may move under
// this device's own earlier writes landing, which the move follows. Once the
// server has answered it, a copy stamped after the move's own stamp is a later
// change made elsewhere (a reopen, a start), which shows (X3, Z2, stage 6
// review 7).
export function ownStatusMoveHolds(mv, m) {
    if (!mv || !m || m.status === mv.status) return false;
    if (!mv.landed) return true;
    // A feed copy past the answer's score version is newer whatever its stamp
    // (stage 6 review 9, F7).
    if (Number.isFinite(m.scoreVersion) && Number.isFinite(mv.version) && m.scoreVersion > mv.version) return false;
    return !(m.modifiedAt > mv.stamp);
}

// ownStatusMoveEnded: whether the move `mv` the server answered is over for
// the feed's copy `m`, so its owner forgets it (the court console's forget
// effect). A copy in a different status ends it once ownStatusMoveHolds no
// longer holds (a later change made elsewhere). A copy that already reads the
// move's status ends it only when it is not older than the move: stamped at or
// after the move's own stamp, or past the answer's score version (strictly: a
// start or a send back can leave the version where it was). An older copy can
// read the same status by chance (bc-sssf: the pre-start copy of a bout this
// console started and then sent back reads scheduled), and forgetting the move
// on it let a refetch answered between the start and the send back show the
// bout running again.
export function ownStatusMoveEnded(mv, m) {
    if (!mv || !mv.landed || !m) return false;
    if (m.status !== mv.status) return !ownStatusMoveHolds(mv, m);
    if (Number.isFinite(m.scoreVersion) && Number.isFinite(mv.version) && m.scoreVersion > mv.version) return true;
    return m.modifiedAt >= mv.stamp;
}

// withOwnStatusMove: match `m` as the court console shows it under the move
// `mv`: sent back reads scheduled, its score kept; finished reads completed
// with the winner the finish names (an engi finish, which names none: the side
// its flags decide), or for a decision the side that did not withdraw, the
// finish's counts laid on, so the court row never shows a finished match
// without its winner. The same after a reload or with the console mounted
// again: the move is read from the outbox, never from the page's memory.
export function withOwnStatusMove(m, mv) {
    if (!ownStatusMoveHolds(mv, m)) return m;
    if (mv.status === "scheduled") return { ...m, status: "scheduled" };
    const p = mv.payload || {};
    let winner;
    if (mv.kind === "decision") {
        const lost = sideKeyForDecisionBy(p.decisionBy);
        winner = lost === "a" ? m.sideB : lost === "b" ? m.sideA : null;
    } else {
        winner = finishWinnerOf(m, p);
        // The wire names the winner by name: the side it names.
        if (typeof winner === "string") {
            if (winner === bracketSideName(m.sideA)) winner = m.sideA;
            else if (winner === bracketSideName(m.sideB)) winner = m.sideB;
        }
    }
    return { ...m, ...finishFieldsOf(p), status: "completed", ...(winner ? { winner } : {}) };
}

// finishWinnerOf: the winner a finish names, or, for an engi finish, which
// names none (the server decides it from the flags, engine.engiWinnerSide),
// the side with more flags in it. The console reads a match finished here as
// finished with this winner (withOwnStatusMove) and advances its local bracket on
// it, so the court row never shows a finished match without its winner
// (stage 6).
export function finishWinnerOf(match, patch) {
    if (!match || !patch) return null;
    if (patch.winner) return patch.winner;
    const a = Number(patch.flagsA);
    const b = Number(patch.flagsB);
    if (!Number.isFinite(a) || !Number.isFinite(b) || a === b) return null;
    return a > b ? match.sideA : match.sideB;
}

// finishFieldsOf: what a finish states beside its verdict that the court row
// shows (an engi finish's counts), laid on the match while it reads as
// finished here, so the row's flags and its winner come from the same finish.
function finishFieldsOf(patch) {
    if (!patch) return null;
    const a = Number(patch.flagsA);
    const b = Number(patch.flagsB);
    return Number.isFinite(a) && Number.isFinite(b) ? { flagsA: a, flagsB: b } : null;
}

const _WINNER_OF_RE = /^Winner of r(\d+)-m(\d+)$/;

// _findMatchInRounds locates a match by id, returning its {R (round), M (index)}
// position, or {R:-1, M:-1} if absent. Shared by propagateBracketWinnerLocal and
// applyBronzeLoserLocal so the search logic can't drift between them.
function _findMatchInRounds(rounds, matchId) {
    for (let r = 0; r < rounds.length; r++) {
        const idx = (rounds[r] || []).findIndex((x) => x && x.id === matchId);
        if (idx >= 0) return { R: r, M: idx };
    }
    return { R: -1, M: -1 };
}

// propagateBracketWinnerLocal advances a winner into the next round's placeholder
// side on the CLIENT (mp-y3nk offline console), so a court running fully offline
// can complete a bout and have the next match: including the final: become
// runnable without a server round-trip. Mirrors the Go engine
// propagateBracketWinner positional rule: a completed match at rounds[r][m] feeds
// rounds[r+1][floor(m/2)], filling sideA when m is even and sideB when m is odd.
// The winning SIDE object (carrying id + name, not just the name) is copied up so
// downstream highlight/scoring keys keep working. Pure + immutable: returns fresh
// rounds and never mutates the input. The server remains authoritative; on
// reconnect a refetch replaces this optimistic tree with the real one.
export function propagateBracketWinnerLocal(rounds, matchId, winnerName) {
    if (!Array.isArray(rounds) || !matchId) return rounds;
    const { R, M } = _findMatchInRounds(rounds, matchId);
    if (R < 0) return rounds; // unknown id → no-op (identity preserved)

    const match = rounds[R][M];
    const aName = bracketSideName(match.sideA);
    const bName = bracketSideName(match.sideB);
    // The winning side OBJECT, so id-based highlighting survives advancement.
    const winnerSide = winnerName === aName ? match.sideA
        : winnerName === bName ? match.sideB
        : winnerName;

    // Shallow-clone every match so the returned tree shares no references with
    // the input (immutability for React state).
    const next = rounds.map((round) => round.map((x) => ({ ...x })));
    next[R][M].winner = winnerSide;
    next[R][M].status = "completed";

    if (R < rounds.length - 1) {
        const nm = next[R + 1][Math.floor(M / 2)];
        if (nm) {
            if (M % 2 === 0) nm.sideA = winnerSide; else nm.sideB = winnerSide;
            // Both sides now real (no "Winner of" placeholder) → the match is
            // runnable; promote a pending/blank status to scheduled.
            const na = bracketSideName(nm.sideA);
            const nb = bracketSideName(nm.sideB);
            const bothResolved = na && nb && !_WINNER_OF_RE.test(na) && !_WINNER_OF_RE.test(nb);
            if (bothResolved && (nm.status === "pending" || nm.status === "scheduled" || !nm.status)) {
                nm.status = "scheduled";
            }
        }
    }
    return next;
}

// applyBronzeLoserLocal places the LOSER of a completed semifinal into the
// client-side thirdPlaceMatch, mirroring the Go engine's bronze-seeding rule
// (naginata only). Only acts when the scored match is in the SEMIFINAL round
// (R === rounds.length - 2) and a thirdPlaceMatch object is provided. Assigns
// to thirdPlaceMatch.sideA when M is even, sideB when M is odd. Returns an
// immutably cloned thirdPlaceMatch, or null when conditions are not met (not a
// semifinal, no thirdPlaceMatch, unresolved/placeholder loser). Pure + immutable:
// caller merges the result into the bracket; this function never mutates inputs.
export function applyBronzeLoserLocal(rounds, matchId, winnerName, thirdPlaceMatch) {
    if (!thirdPlaceMatch || !Array.isArray(rounds) || !matchId) return null;
    const { R, M } = _findMatchInRounds(rounds, matchId);
    // Only act for the semifinal round (one step before the final).
    if (R < 0 || R !== rounds.length - 2) return null;
    const match = rounds[R][M];
    const aName = bracketSideName(match.sideA);
    const bName = bracketSideName(match.sideB);
    // Derive the loser side object (carrying id + name, not just the name).
    let loserSide;
    if (winnerName === aName) {
        loserSide = match.sideB;
    } else if (winnerName === bName) {
        loserSide = match.sideA;
    } else {
        return null; // winner doesn't match either side → no-op
    }
    const loserName = bracketSideName(loserSide);
    // Reject empty or still-placeholder losers: a "Winner of rX-mY" loser means
    // the semifinal side itself was unresolved and should not be seeded.
    if (!loserName || _WINNER_OF_RE.test(loserName)) return null;
    // Immutably assign the loser to the correct bronze side.
    if (M % 2 === 0) {
        return { ...thirdPlaceMatch, sideA: loserSide };
    }
    return { ...thirdPlaceMatch, sideB: loserSide };
}

// makeReconnectRefetcher builds an SSE onStatus handler that fires onReconnect()
// only on an 'open' that FOLLOWS an 'error' (a genuine reconnect), never on the
// first connect (mp-y3nk Phase 2). A court whose tablet dropped offline can miss
// events while disconnected (e.g. a feeder resolved on another court); refetching
// on reconnect self-heals its queue instead of waiting for the next ordinary
// event. Gating on a prior error avoids a redundant double-fetch on page load,
// where the mount fetch already runs. Exported for unit testing.
//
// markLost() counts as such an error. The stream does not always error when the
// device loses its connection: app.jsx reopens it when the device comes back
// online (reconnectEvents closes the old one, which fires no error), so that
// reopen arrives as an 'open' with nothing before it, and a court that went
// offline kept its stale queue (offering Start for a match another device had
// started) until a manual refresh. The console marks the stream lost on
// 'online', and the reopen that follows refetches. The reopen, not 'online'
// itself, is the moment to fetch: it is when the device is known to reach the
// server again.
export function makeReconnectRefetcher(onReconnect) {
    let sawError = false;
    const onStatus = (status) => {
        if (status === "error") { sawError = true; return; }
        if (status === "open" && sawError) { sawError = false; onReconnect(); }
    };
    onStatus.markLost = () => { sawError = true; };
    return onStatus;
}
