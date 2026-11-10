// bc-obox G4b: the tournament settings and a competition's fighting-spirit
// awards, whole documents saved against a version through the outbox. Split
// out of api_client.jsx: the one owner of how a document save keeps its
// version, shows while queued and is refused. The outbox hands in the three
// things it needs (createDocumentVersions); nothing here touches its queue
// otherwise.
//
// One base per document (`tournament`, `awards:<comp>`). A save not yet out
// takes the place of a queued save of the same document (_coalesceTarget in
// api_client.jsx); a save made against the version this device's own last save
// of the document replaced takes the version that save answered
// (rebaseQueuedVersion), so the device never refuses its own earlier save; a
// save made against any other version is sent as it is, and the server refuses
// it when another device changed the document since (409 tournament_changed /
// awards_changed).

export const DOC_VERSION_FIELD = { tournament: 'tournamentVersion', awards: 'awardsVersion' };
export const TOURNAMENT_BASE = 'tournament';
const DOC_MOVES_MAX = 32;

export function awardsBase(compID) {
    return `awards:${compID}`;
}

// The fields of a queued document save that are the document, not the
// outbox's tags, the version it was made against or the password. The sponsors
// are not the save's: the server carries the stored list over, so a list
// riding the payload is only the form's older copy.
function documentFields(payload) {
    const { writeId: _w, session: _s, basis: _b, password: _p, tournamentVersion: _t, awardsVersion: _a, competitions: _c, sponsors: _sp, ...fields } = payload || {};
    return fields;
}

// onDocumentSettled: what a screen does when its document save is answered.
// A save that takes the place of a queued one shares its `settled` promise
// (_openSettlement), so each screen's save would otherwise attach one more
// handler and show its answer once per save. The promise gets one pair of
// handlers, and the latest save's pair is the one that runs: its closure knows
// the most of what was typed.
const settledHandlers = new WeakMap();
export function onDocumentSettled(settled, onAnswer, onRefusal) {
    const known = settledHandlers.get(settled);
    if (known) { known.onAnswer = onAnswer; known.onRefusal = onRefusal; return; }
    const handlers = { onAnswer, onRefusal };
    settledHandlers.set(settled, handlers);
    settled.then((v) => handlers.onAnswer(v), (e) => handlers.onRefusal(e));
}

// tournamentRefusal: the Error a refused tournament save rejects with, the
// shape the direct save threw: a 409 tournament_changed carries the settings
// as they stand now, for the open form to reload.
export function tournamentRefusal(err, status) {
    const e = new Error(err.error || 'Failed to update tournament');
    if (status === 409 && err.code === 'tournament_changed') {
        e.code = 'tournament_changed';
        e.tournament = err.tournament || null;
    }
    return e;
}

// awardsRefusal: the same for an awards save: a 409 awards_changed carries
// the list as it stands now and its version.
export function awardsRefusal(err, status) {
    const e = new Error(err.error || 'Failed to update competition awards');
    if (status === 409 && err.code === 'awards_changed') {
        e.code = 'awards_changed';
        e.awardsVersion = err.awardsVersion || '';
        e.fightingSpiritAwards = err.fightingSpiritAwards || [];
    }
    return e;
}

// createDocumentVersions: the stateful half, over the outbox's queue.
//   entriesFor(base)  the queued [key, descriptor] pairs of a base, in order
//   isInFlight(d)     whether a descriptor is out on the network now
//   persistQueue()    write the queue to storage after its payloads changed
export function createDocumentVersions({ entriesFor, isInFlight, persistQueue }) {
    // Per base: the versions this device's landed saves moved the document from,
    // each to the version that save answered. In memory: a reload reads the
    // document again.
    const docVersionMoves = new Map();

    // currentDocVersion: `v` moved along this device's own landed saves.
    function currentDocVersion(base, v) {
        const moves = docVersionMoves.get(base);
        let out = v;
        for (let i = 0; moves && i < DOC_MOVES_MAX && moves.has(out); i++) out = moves.get(out);
        return out;
    }

    // rebaseQueuedVersion: the ONE rule for both documents. A save of `base` that
    // was sent at version `sent` landed and answered `got`: a save of the same
    // document still queued at `sent` (made before the answer came) takes `got`,
    // as does a save made later at `sent`.
    function rebaseQueuedVersion(base, field, sent, got) {
        if (!field || typeof sent !== 'string' || !sent || typeof got !== 'string' || !got || sent === got) return;
        let moves = docVersionMoves.get(base);
        if (!moves) { moves = new Map(); docVersionMoves.set(base, moves); }
        // A version is a hash of the document, so a save can put it back at content
        // it had before. The document is at `got` now: a move out of `got` made
        // earlier describes a document that no longer exists, and following it
        // would send a version the server no longer holds.
        moves.delete(got);
        moves.delete(sent);
        moves.set(sent, got);
        while (moves.size > DOC_MOVES_MAX) moves.delete(moves.keys().next().value);
        let changed = false;
        for (const [, d] of entriesFor(base)) {
            if (d && d.payload && !isInFlight(d) && d.payload[field] === sent) {
                d.payload[field] = got;
                changed = true;
            }
        }
        if (changed) persistQueue();
    }

    // pendingDocument: the payload of the save still queued for a document (its
    // last entry), or null.
    function pendingDocument(base) {
        const list = entriesFor(base);
        return list.length ? list[list.length - 1][1].payload || null : null;
    }

    // overlayPendingTournament: the fetched tournament with the settings this
    // device still has queued put on, so a refetch before the save lands never
    // shows the stored copy over the operator's change.
    function overlayPendingTournament(t) {
        const p = t && typeof t === 'object' ? pendingDocument(TOURNAMENT_BASE) : null;
        return p ? { ...t, ...documentFields(p) } : t;
    }

    // overlayPendingAwards: the same for a competition's awards.
    function overlayPendingAwards(comp, id) {
        const p = pendingDocument(awardsBase(id));
        if (!p || !Array.isArray(p.fightingSpiritAwards)) return comp;
        const out = { ...comp };
        if (comp.config) out.config = { ...comp.config, fightingSpiritAwards: p.fightingSpiritAwards };
        if (!comp.config || 'fightingSpiritAwards' in comp) out.fightingSpiritAwards = p.fightingSpiritAwards;
        return out;
    }

    return { currentDocVersion, rebaseQueuedVersion, overlayPendingTournament, overlayPendingAwards };
}
