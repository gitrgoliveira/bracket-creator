// minted_members.jsx: the ONE owner of what is known about a team member id
// this device (or another tab of it) minted for a member add (bc-evts stage 3,
// operator decision M1 (a), 2026-10-17). A leaf: no imports, no DOM, no
// network. api_client.jsx creates one per page (`createMintedMembers`), asks
// it before sending every write that names a member id (`gate`), tells it what
// came of every add (`sent`, `answered`, `discarded`) and of every members
// list (`listed`, `readEnded`), and does the reads it asks for.
//
// One record per id, shared by every tab through browser storage
// (MINTED_MEMBERS_KEY), so every tab gates and strips the same way:
//
//   queued --send--> out --2xx, or a members list holding the id--> kept (record forgotten)
//   queued|out --4xx on its merits--> dropped
//   out --5xx, 401, timeout--> queued again while the add is still queued (it is sent again)
//   out --member_id_conflict, or the add leaves the queue unanswered--> unknown
//   queued --discarded: never went out--> dropped; went out--> unknown
//   unknown --no tab has the add out, the grace after a timeout over--> read
//          --the list holds the id--> kept; --it does not--> dropped
//          --the read fails--> unknown, and the writes go as they were made
//
// gate(write): every id it names (its payload, and the member a rename or a
// cleared name is addressed to) that is queued, out or unknown holds the write
// back ('wait'); a kept id (no record) lets it go as made; a dropped one comes
// off it (a numbered row or a lineup position keeps its name, a
// representative pick is cleared, a tap loses its memberId), and a rename or a
// cleared name of a dropped member leaves the queue unsent ('leave').
//
// An id is taken off a write only once the member provably does not exist: a
// refusal on its merits, an add that never went out, or a read made once no
// tab has the add out (and FETCH_TIMEOUT_MS after a request given up on, since
// such an add may still commit) whose list lacks the id. Storage that refuses
// is never a stall: this tab keeps what it learned in memory and a write
// nobody can judge goes as it was made, for the server to judge.

export const MINTED_MEMBERS_KEY = 'bc_minted_members_v1';
// The key the notes of dropped ids were kept under before this owner: read
// once as dropped records, then removed.
export const LEGACY_DROPPED_MEMBERS_KEY = 'bc_dropped_member_ids_v1';
// A settled record nothing names is forgotten this long after its last change.
export const MINTED_KEEP_MS = 10 * 60 * 1000;
// Past its deadline a request still needs a moment to be given up on.
const OUT_SLACK_MS = 1000;

const SUB_RESULT_MEMBER_FIELDS = ['sideAMemberId', 'sideBMemberId', 'winnerMemberId'];

// mintedMemberUrlId: the member a rename or a cleared name is addressed to
// (`.../teams/<team>/members/<id>`), '' for any other write.
export function mintedMemberUrlId(d) {
    if (!d || d.kind !== 'member' || (d.method !== 'PUT' && d.method !== 'DELETE')) return '';
    const m = String(d.url || '').match(/\/members\/([^/?]+)$/);
    return m ? decodeURIComponent(m[1]) : '';
}

// mintedAddId: the id a member add mints, '' for any other write.
export function mintedAddId(d) {
    return d && d.kind === 'member' && d.method === 'POST' && d.payload && typeof d.payload.id === 'string'
        ? d.payload.id : '';
}

// mintedIdsNamed: every member id a queued write names: a lineup's
// `memberIds`, a tap's `memberId` (a fighter or representative pick), a score
// write's bout rows (both sides and the winner), and the member a rename or a
// cleared name is addressed to. The ONE reading of "the ids a write names".
export function mintedIdsNamed(d) {
    const ids = new Set();
    const urlId = mintedMemberUrlId(d);
    if (urlId) ids.add(urlId);
    const payload = d && d.payload;
    if (!payload || typeof payload !== 'object') return ids;
    if (payload.memberIds && typeof payload.memberIds === 'object') {
        for (const v of Object.values(payload.memberIds)) if (typeof v === 'string' && v) ids.add(v);
    }
    if (typeof payload.memberId === 'string' && payload.memberId) ids.add(payload.memberId);
    if (Array.isArray(payload.subResults)) {
        for (const row of payload.subResults) {
            if (!row || typeof row !== 'object') continue;
            for (const f of SUB_RESULT_MEMBER_FIELDS) if (typeof row[f] === 'string' && row[f]) ids.add(row[f]);
        }
    }
    return ids;
}

// mintedStrip: takes the id off the write's payload, in place; true when it
// changed. A lineup position keeps its name; a tap's fighter pick keeps its
// `name` and a representative pick with no member is cleared, as the server
// reads an event with no `memberId`; a numbered bout row keeps its fighter's
// name, and since the representative row's names are the TEAM names its pick
// is cleared; a winner named by the id goes with it (the row's names still say
// who won).
export function mintedStrip(d, id) {
    const payload = d && d.payload;
    if (!payload || typeof payload !== 'object') return false;
    let changed = false;
    if (payload.memberIds && typeof payload.memberIds === 'object') {
        for (const pos of Object.keys(payload.memberIds)) {
            if (payload.memberIds[pos] === id) { delete payload.memberIds[pos]; changed = true; }
        }
    }
    if (payload.memberId === id) { delete payload.memberId; changed = true; }
    if (Array.isArray(payload.subResults)) {
        for (const row of payload.subResults) {
            if (!row || typeof row !== 'object') continue;
            for (const f of SUB_RESULT_MEMBER_FIELDS) if (row[f] === id) { row[f] = ''; changed = true; }
        }
    }
    return changed;
}

// mintedProofCovers: a dropped record `r` is the proof for its competition
// and, for a write that names a team (`team`, '' when it names none), its
// team.
export function mintedProofCovers(r, d, team) {
    if (r.c && (d && d.compID) !== r.c) return false;
    return !(team && r.t && team !== r.t);
}

function parseRecords(raw) {
    if (!raw) return {};
    try {
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
        const out = {};
        for (const [id, r] of Object.entries(parsed)) {
            if (r && typeof r === 'object' && typeof r.s === 'string') out[id] = r;
        }
        return out;
    } catch (_e) {
        return {};
    }
}

/**
 * @param {{
 *   storage: {get: (k: string) => (string|null), set: (k: string, v: string) => void, remove: (k: string) => void},
 *   now?: () => number, timeoutMs: number, tab: string,
 * }} opts storage may throw on any call; `tab` names this page.
 */
export function createMintedMembers({ storage, now = () => Date.now(), timeoutMs, tab }) {
    // The records as last read or made here: the fallback when storage
    // cannot be read. A change storage refused is kept in `unsaved` and laid
    // over what storage holds, so what this tab learned is never lost to it.
    let mem = {};
    const unsaved = new Map();

    function read() {
        let raw;
        try {
            raw = storage.get(MINTED_MEMBERS_KEY);
        } catch (_e) {
            return mem;
        }
        const recs = parseRecords(raw);
        for (const [id, r] of unsaved) {
            if (r) recs[id] = r; else delete recs[id];
        }
        mem = recs;
        return mem;
    }

    function write(recs) {
        try {
            if (Object.keys(recs).length === 0) storage.remove(MINTED_MEMBERS_KEY);
            else storage.set(MINTED_MEMBERS_KEY, JSON.stringify(recs));
            unsaved.clear();
            return true;
        } catch (_e) {
            return false;
        }
    }

    // change: read, change the one record, write back. `fn` returns the new
    // record, null to forget it, or undefined for no change.
    function change(id, fn) {
        if (!id) return false;
        const recs = read();
        const next = fn(recs[id] ? { ...recs[id] } : undefined);
        if (next === undefined) return false;
        if (next) recs[id] = next; else delete recs[id];
        if (!write(recs)) unsaved.set(id, next || null);
        return true;
    }

    // fresh: the record of an add `d` just queued: its competition and the
    // team its URL names.
    const fresh = (d) => {
        const team = (String((d && d.url) || '').match(/\/teams\/([^/]+)\/members$/) || [])[1] || '';
        return { c: (d && d.compID) || '', t: decodeURIComponent(team), s: 'queued', at: now() };
    };
    const dropped = (r) => ({ c: r.c, t: r.t, s: 'dropped', at: now() });
    const unknown = (r, readAfter) => ({ c: r.c, t: r.t, s: 'unknown', at: now(), w: 1, ra: Math.max(Number(r.ra) || 0, readAfter) });

    // A request this record says is out is over by its deadline, whoever sent it.
    const outEndsAt = (r) => (Number(r.sa) || 0) + timeoutMs + OUT_SLACK_MS;

    // judge: what is known of `id` (its record `r`) at `t`, one reading for
    // the gate and for a read's answer:
    //   'dropped'  the member never was;
    //   'wait'     the add still decides (queued, or out: `until` when only a
    //              deadline ends it), or the grace after a request given up
    //              on is not over (`until`);
    //   'read'     nothing will answer the add: the team's members say;
    //   'reread'   a read failed: the writes go as they were made (the rule),
    //              and the members are asked again now and then;
    //   'go'       the same, between those reads.
    function judge(id, r, ctx, t) {
        if (r.s === 'dropped') return { kind: 'dropped' };
        if (r.s === 'out' && (r.tab === tab ? ctx.addOutHere(id) : t < outEndsAt(r))) {
            return { kind: 'wait', until: r.tab === tab ? 0 : outEndsAt(r) };
        }
        if (ctx.addQueued(id)) return { kind: 'wait', until: 0 };
        if (r.rf) return { kind: t - Number(r.rf) >= timeoutMs ? 'reread' : 'go' };
        // An out nobody answered ended at its deadline; the add may commit up
        // to a deadline after that.
        const readAfter = Math.max(Number(r.ra) || 0, r.s === 'out' ? outEndsAt(r) + timeoutMs : 0);
        if (t < readAfter) return { kind: 'wait', until: readAfter };
        return { kind: 'read' };
    }

    // The notes an older page left (LEGACY_DROPPED_MEMBERS_KEY) are proofs the
    // member never was: taken as dropped records, once.
    (function takeLegacyNotes() {
        let raw = null;
        try { raw = storage.get(LEGACY_DROPPED_MEMBERS_KEY); } catch (_e) { return; }
        if (raw === null) return;
        let notes = [];
        try { notes = JSON.parse(raw); } catch (_e) { notes = []; }
        if (Array.isArray(notes)) {
            for (const n of notes) {
                if (!n || typeof n.id !== 'string' || !n.id) continue;
                change(n.id, (r) => (r ? undefined : { c: n.competitionId || '', t: n.teamId || '', s: 'dropped', at: now() }));
            }
        }
        try { storage.remove(LEGACY_DROPPED_MEMBERS_KEY); } catch (_e) { /* ignore */ }
    })();

    return {
        // minted: the add `d` was queued under its minted id.
        minted(d) {
            change(mintedAddId(d), (r) => (r ? undefined : fresh(d)));
        },

        // sent: the add's request goes out from this tab. An add queued with
        // no record (storage refused it then) gets one now.
        sent(d) {
            change(mintedAddId(d), (r) => (r && r.s === 'dropped'
                ? undefined : { ...(r || fresh(d)), s: 'out', at: now(), sa: now(), tab, w: 1 }));
        },

        // answered: what came of the add's request. `outcome` is 'kept' (a
        // 2xx), 'refused' (a 4xx on its merits), 'held' (member_id_conflict:
        // the id is held, by this team or elsewhere), 'retry' (a 5xx, 429, 403
        // or 401, or no connection) or 'timeout' (given up on at its
        // deadline: a server still at work may keep it after that, so a read
        // waits a deadline more). `stillQueued`: the add is still queued, to
        // be sent again.
        // Answers 'dropped' when the member is now known never to have been.
        answered(d, outcome, stillQueued) {
            let result = '';
            // No record: a logout's wipe took it while the request was out
            // (`sent` made one for every add that went out).
            change(mintedAddId(d), (r) => {
                if (!r) return undefined;
                if (outcome === 'kept') return null;
                if (r.s === 'dropped') return undefined;
                if (outcome === 'refused') { result = 'dropped'; return dropped(r); }
                const grace = outcome === 'timeout' ? now() + timeoutMs : now();
                if (outcome === 'held' || !stillQueued) return unknown(r, grace);
                return { c: r.c, t: r.t, s: 'queued', at: now(), w: 1, ...(outcome === 'timeout' || r.ra ? { ra: Math.max(Number(r.ra) || 0, grace) } : {}) };
            });
            return result;
        },

        // discarded: the add left this tab's queue unanswered (an operator's
        // discard while no request of this tab's is out, or an entry no
        // request can carry; one discarded while out is answered by its
        // request's end). Another tab's request still out answers it there.
        // Answers 'dropped' when the add never went out. An add with no
        // record (storage refused it) may have gone out: unknown.
        discarded(d) {
            let result = '';
            change(mintedAddId(d), (known) => {
                const r = known || { ...fresh(d), w: 1 };
                if (r.s === 'dropped' || r.s === 'unknown') return undefined;
                if (r.s === 'out' && r.tab !== tab && now() < outEndsAt(r)) return undefined;
                if (!r.w) { result = 'dropped'; return dropped(r); }
                return unknown(r, r.s === 'out' ? outEndsAt(r) + timeoutMs : now());
            });
            return result;
        },

        // listed: a members list the server answered for `competitionId`
        // ({teamId: [member]}): every id it holds is kept, whatever was known.
        listed(teams, competitionId) {
            if (!teams || typeof teams !== 'object' || !competitionId) return;
            for (const [id, r] of Object.entries(read())) {
                const list = r.c === competitionId && Array.isArray(teams[r.t]) ? teams[r.t] : null;
                if (list && list.some((m) => m && m.id === id)) change(id, () => null);
            }
        },

        // readEnded: the read `gate` asked for answered `list` (the team's
        // members), or failed (null). Taken only while the record still asks
        // for it (`ctx` as for `gate`): an add queued or sent again since
        // decides by its answer instead. Answers 'dropped' when the list
        // proved the member never was.
        readEnded(id, list, ctx) {
            let result = '';
            change(id, (r) => {
                if (!r) return undefined;
                const v = judge(id, r, ctx, now());
                if (v.kind !== 'read' && v.kind !== 'reread') return undefined;
                if (list === null) return { ...r, rf: now() };
                if (list.some((m) => m && m.id === id)) return null;
                result = 'dropped';
                return dropped(r);
            });
            return result;
        },

        // gate: what the write `d` must do now about the ids it names.
        //   ctx.addQueued(id): this tab or storage still holds the add.
        //   ctx.addOutHere(id): this tab's request for the add is out.
        //   ctx.teamOf(d): the team a write names, '' for one that names none.
        // Answers {wait, until, reads: [{id, c, t}], strip: [id], leave}.
        gate(d, ctx) {
            const res = { wait: false, until: 0, reads: [], strip: [], leave: false };
            const ids = mintedIdsNamed(d);
            if (ids.size === 0) return res;
            const recs = read();
            const t = now();
            const urlId = mintedMemberUrlId(d);
            const later = (at) => { res.until = res.until ? Math.min(res.until, at) : at; };
            for (const id of ids) {
                const r = recs[id];
                if (!r) continue;
                const v = judge(id, r, ctx, t);
                if (v.kind === 'dropped') {
                    if (!mintedProofCovers(r, d, ctx.teamOf(d))) continue;
                    if (id === urlId) res.leave = true; else res.strip.push(id);
                } else if (v.kind === 'wait') {
                    res.wait = true;
                    if (v.until) later(v.until);
                } else if (v.kind === 'read') {
                    res.wait = true;
                    res.reads.push({ id, c: r.c, t: r.t });
                } else if (v.kind === 'reread') {
                    res.reads.push({ id, c: r.c, t: r.t });
                }
            }
            return res;
        },

        // due: of `ids` (the adds this tab sent or discarded), those whose
        // members may be asked now with no write waiting on them, so the
        // editors hear soon that a member never was, and the earliest time
        // one of the others may be (`until`, 0 for none).
        due(ids, ctx) {
            const res = { reads: [], until: 0 };
            const recs = read();
            const t = now();
            for (const id of ids) {
                const r = recs[id];
                if (!r) continue;
                const v = judge(id, r, ctx, t);
                if (v.kind === 'read') res.reads.push({ id, c: r.c, t: r.t });
                else if (v.kind === 'wait' && v.until) res.until = res.until ? Math.min(res.until, v.until) : v.until;
            }
            return res;
        },

        // recordOf: the record of `id`, or null when nothing is known.
        recordOf(id) {
            const r = read()[id];
            return r ? { ...r } : null;
        },

        // onStored: another tab changed the records (a storage event). Answers
        // `dropped`, the ids that became dropped there, and `unblocks`: whether
        // a write held back for an id may now go or ask the members (a record
        // forgotten, dropped, unknown, or whose read failed). An add sent or
        // queued again there frees nothing, so it does not hurry this tab's
        // own retries, which keep their backoff.
        onStored(oldRaw, newRaw) {
            const before = parseRecords(oldRaw);
            const after = parseRecords(newRaw);
            const dropped = [];
            let unblocks = false;
            for (const [id, r] of Object.entries(after)) {
                const was = before[id];
                if (r.s === 'dropped' && !(was && was.s === 'dropped')) dropped.push({ id, c: r.c, t: r.t });
                if (r.s === 'unknown' && (!was || was.s !== 'unknown' || was.rf !== r.rf)) unblocks = true;
            }
            for (const id of Object.keys(before)) if (!after[id]) unblocks = true;
            read();
            return { dropped, unblocks: unblocks || dropped.length > 0 };
        },

        // prune: forgets a settled record nothing names (`named`: every id a
        // queued write names, this tab's and the stored ones; `adds`: every id
        // an add still queued mints) once it is MINTED_KEEP_MS old.
        prune(named, adds) {
            const t = now();
            for (const [id, r] of Object.entries(read())) {
                if (named.has(id) || adds.has(id)) continue;
                if (r.s === 'out' && t < outEndsAt(r)) continue;
                if (t - (Number(r.at) || 0) < MINTED_KEEP_MS) continue;
                change(id, () => null);
            }
        },

        // clear: the logout wipe; the records go with the queue they belong to.
        clear() {
            mem = {};
            unsaved.clear();
            try { storage.remove(MINTED_MEMBERS_KEY); } catch (_e) { /* ignore */ }
            try { storage.remove(LEGACY_DROPPED_MEMBERS_KEY); } catch (_e) { /* ignore */ }
        },
    };
}
