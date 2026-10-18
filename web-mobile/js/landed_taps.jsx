// landed_taps.jsx: the ONE owner of what this device knows of its own taps
// the server answered as landed (applied, recorded, duplicate), so one it
// turns away LATER (a finish that never saw it reaches the server after it,
// rule D5) is told to the operator once (stage 6 reviews 5 to 7). A leaf: it
// reads and writes only the storage it is handed.
//
// - One record per match (LANDED_TAPS_PREFIX + match), never one for the
//   whole device: a tap batch's answer reads and writes its own match's
//   record only, a few hundred bytes, never the device's whole history (the
//   single record grew to ~170 KB, parsed and rewritten on every batch). An
//   index key lists the match records, so a sign-out can clear them all.
// - Each entry names the tab that noted it (`tab`). Every tab of the device
//   sees the same copies of a match, so only one may tell a tap: the tab that
//   noted it while that tab is shown and alive (it writes its heartbeat to
//   LANDED_TABS_KEY every LANDED_TAB_BEAT_MS, and as its page is hidden or
//   shown), else the shown tab with the lowest id (with none shown, a gone
//   tab's taps go to the live tab with the lowest id). A hidden page is alive
//   for LANDED_TAB_HIDDEN_LIVE_MS after its last beat, since its timers may
//   stop, which keeps its entries from a sign-out and the sweep, never from
//   being told by a shown tab (stage 6 review 10, M1). A tab that tells takes the entry out of the record first, so
//   no later copy tells it again (one notice per tap per device). Nothing
//   waits: a tap no tab may tell yet stays in the record for the next copy.
// - Storage that refuses a write keeps the record in memory for that key;
//   memory is read only while storage still lacks this tab's write, so a
//   record another tab's sign-out removed is never put back.
// - Kept a day, at most LANDED_TAPS_PER_MATCH of a match and LANDED_TAPS_MAX
//   in all (the sweep, which reaches every match record, not only one noted
//   again), so the record has a fixed ceiling beside the outbox in the same
//   storage (stage 6 review 8, M3).
// - A sign-out takes this tab's entries and those of tabs no longer alive;
//   another live tab's stay for it (m10).

export const LANDED_TAPS_PREFIX = 'bc_landed_taps_v2:';
export const LANDED_TAPS_INDEX = 'bc_landed_taps_v2';
export const LANDED_TAPS_LEGACY_KEY = 'bc_landed_taps_v1';
export const LANDED_TABS_KEY = 'bc_landed_tabs_v1';
export const LANDED_TAPS_KEEP_MS = 24 * 60 * 60 * 1000;
export const LANDED_TAPS_PER_MATCH = 500;
// About 150 characters an entry: some 300 K characters at most in all.
export const LANDED_TAPS_MAX = 2000;
export const LANDED_TAPS_SWEEP_MS = 5 * 60 * 1000;
export const LANDED_TAB_BEAT_MS = 20_000;
export const LANDED_TAB_LIVE_MS = 3 * LANDED_TAB_BEAT_MS;
// A tab in the background (its page hidden) is alive this long after its last
// beat: a browser slows or stops a hidden page's timers, so its beat cannot
// say it is alive (stage 6 review 9, F6: a phone's backgrounded tab looked
// gone after a minute, and a sign-out in another tab took its entries). It
// beats as it is hidden and as it is shown again; a page that goes removes its
// beat (pagehide), so only a tab killed in the background waits this long.
export const LANDED_TAB_HIDDEN_LIVE_MS = 30 * 60 * 1000;

// Whether a tab's beat says it is alive at t: a number (a visible tab's beat,
// and every beat an older page wrote) or { at, hidden }.
function beatAlive(beat, t) {
    if (beat && typeof beat === 'object') {
        return t - Number(beat.at) < (beat.hidden ? LANDED_TAB_HIDDEN_LIVE_MS : LANDED_TAB_LIVE_MS);
    }
    return t - Number(beat) < LANDED_TAB_LIVE_MS;
}

// Whether a tab's beat says it is shown and alive at t: only such a tab is
// sure to tell its taps now, since a hidden page's timers may have stopped.
function beatVisible(beat, t) {
    if (beat && typeof beat === 'object') return !beat.hidden && t - Number(beat.at) < LANDED_TAB_LIVE_MS;
    return t - Number(beat) < LANDED_TAB_LIVE_MS;
}

const SEP = '\u001f';

export function landedTapsKey(compID, matchID) {
    return LANDED_TAPS_PREFIX + compID + SEP + matchID;
}

/**
 * @param {{ storage: { getItem(k: string): string|null, setItem(k: string, v: string): void, removeItem(k: string): void },
 *   now?: () => number, tab: string }} opts storage may throw on any call.
 */
export function createLandedTaps({ storage, now = () => Date.now(), tab, hidden }) {
    const memory = new Map(); // key -> object, for keys whose write storage refused
    const failed = new Set();

    const parse = (raw, fallback) => {
        try {
            const v = JSON.parse(raw);
            return v && typeof v === 'object' ? v : fallback;
        } catch (_e) {
            return fallback;
        }
    };
    const readObj = (key) => {
        let raw;
        try {
            raw = storage.getItem(key);
        } catch (_e) {
            return { ...memory.get(key) };
        }
        if (raw === null) {
            if (failed.has(key)) return { ...memory.get(key) };
            // Removed (another tab's sign-out), or never written: not put back.
            memory.delete(key);
            return {};
        }
        const v = parse(raw, {});
        return Array.isArray(v) ? {} : v;
    };
    const writeObj = (key, obj) => {
        const empty = Object.keys(obj).length === 0;
        try {
            if (empty) storage.removeItem(key);
            else storage.setItem(key, JSON.stringify(obj));
            failed.delete(key);
            memory.delete(key);
            return true;
        } catch (_e) {
            failed.add(key);
            memory.set(key, obj);
            return false;
        }
    };
    const readIndex = () => {
        let raw = null;
        try { raw = storage.getItem(LANDED_TAPS_INDEX); } catch (_e) { /* memory only */ }
        const stored = raw === null ? [] : parse(raw, []);
        const keys = new Set(Array.isArray(stored) ? stored : []);
        for (const k of memory.keys()) if (k.startsWith(LANDED_TAPS_PREFIX)) keys.add(k);
        return keys;
    };
    const writeIndex = (keys) => {
        try {
            if (keys.size === 0) storage.removeItem(LANDED_TAPS_INDEX);
            else storage.setItem(LANDED_TAPS_INDEX, JSON.stringify([...keys]));
        } catch (_e) { /* the match keys are still found through memory and the scan */ }
    };
    // Every match record: the index, healed by the keys storage holds under
    // the prefix (an index write lost to two tabs racing, or refused for its
    // size, would otherwise leave a record no sweep or sign-out reaches).
    const allKeys = () => {
        const keys = readIndex();
        if (typeof storage.keys === 'function') {
            try {
                for (const k of storage.keys()) if (typeof k === 'string' && k.startsWith(LANDED_TAPS_PREFIX)) keys.add(k);
            } catch (_e) { /* the index alone */ }
        }
        return keys;
    };

    // The tabs alive, by their heartbeat in storage: a hidden one counts, so
    // a sign-out or the sweep never takes its entries.
    const liveTabs = () => {
        const tabs = readObj(LANDED_TABS_KEY);
        const t = now();
        const live = new Set([tab]);
        for (const id of Object.keys(tabs)) if (beatAlive(tabs[id], t)) live.add(id);
        return live;
    };
    // The tabs shown and alive: the ones that tell. Hiddenness protects a
    // tab's record, never its telling (stage 6 review 10, M1: a tap noted in a
    // tab then hidden, its timers stopped, was told by no shown tab for up to
    // LANDED_TAB_HIDDEN_LIVE_MS).
    const tellingTabs = () => {
        const tabs = readObj(LANDED_TABS_KEY);
        const t = now();
        const shown = new Set();
        for (const id of Object.keys(tabs)) if (id !== tab && beatVisible(tabs[id], t)) shown.add(id);
        if (!hiddenNow) shown.add(tab);
        return shown;
    };
    const mayTell = (rec, live, shown) => {
        if (!rec.tab || rec.tab === tab) return true;
        if (shown.has(rec.tab)) return false;
        // Its tab is hidden or gone: the shown tab with the lowest id tells it,
        // and with none shown, a gone tab's taps go to the live tab with the
        // lowest id while a hidden one keeps its own.
        if (shown.size) return [...shown].sort()[0] === tab;
        if (live.has(rec.tab)) return false;
        return [...live].sort()[0] === tab;
    };

    // Rewrites every match record through keep(objs) and heals the index: a
    // record left empty is removed, and the index lists what remains. Only
    // what keep took out is written: each changed record is read again just
    // before its write and loses exactly those entries, so an entry another
    // tab noted while this pass was reading is kept (stage 6 review 9, F6).
    const rewriteAll = (keep) => {
        const index = allKeys();
        const objs = new Map();
        for (const key of index) objs.set(key, readObj(key));
        const before = new Map();
        for (const [key, obj] of objs) before.set(key, { ...obj });
        const changed = keep(objs);
        const emptied = new Set();
        for (const key of changed) {
            const kept = objs.get(key) || {};
            const was = before.get(key) || {};
            const fresh = readObj(key);
            for (const id of Object.keys(was)) {
                if (id in kept) continue;
                // Taken out by this pass: gone, unless another tab noted it
                // again meanwhile (a later `at`).
                if (fresh[id] && Number(fresh[id].at) === Number(was[id] && was[id].at)) delete fresh[id];
            }
            writeObj(key, fresh);
            if (!Object.keys(fresh).length) emptied.add(key);
        }
        const live = new Set();
        for (const key of allKeys()) {
            if (emptied.has(key)) continue;
            const obj = objs.has(key) && !changed.has(key) ? objs.get(key) : readObj(key);
            if (Object.keys(obj).length) live.add(key);
        }
        writeIndex(live);
    };
    // The sweep: entries older than LANDED_TAPS_KEEP_MS go from every match
    // record, not only from a match noted again, and the device keeps at most
    // LANDED_TAPS_MAX entries in all, the latest, so the record never grows
    // past a fixed size whatever the day holds (stage 6 review 8, M3: it
    // shares the origin's storage with the outbox, which it must never crowd
    // out). Run as a match record is made and at most every
    // LANDED_TAPS_SWEEP_MS from the heartbeat.
    let lastSweep = -Infinity;
    const sweep = () => {
        const t = now();
        lastSweep = t;
        rewriteAll((objs) => {
            const changed = new Set();
            const kept = [];
            for (const [key, obj] of objs) {
                for (const id of Object.keys(obj)) {
                    const at = Number(obj[id] && obj[id].at) || 0;
                    if (t - at >= LANDED_TAPS_KEEP_MS) {
                        delete obj[id];
                        changed.add(key);
                    } else {
                        kept.push([at, key, id]);
                    }
                }
            }
            if (kept.length > LANDED_TAPS_MAX) {
                kept.sort((a, b) => a[0] - b[0]);
                for (const [, key, id] of kept.slice(0, kept.length - LANDED_TAPS_MAX)) {
                    delete objs.get(key)[id];
                    changed.add(key);
                }
            }
            return changed;
        });
    };

    // beat({ hidden }) says whether this tab's page is hidden; a beat with no
    // word keeps what was said last. A page made in the background is hidden
    // from its first beat (stage 6 review 10, m6).
    let hiddenNow = typeof hidden === 'boolean'
        ? hidden
        : typeof document !== 'undefined' && document.visibilityState === 'hidden';
    const beat = (opts) => {
        if (opts && typeof opts.hidden === 'boolean') hiddenNow = opts.hidden;
        const tabs = readObj(LANDED_TABS_KEY);
        const t = now();
        for (const id of Object.keys(tabs)) if (id !== tab && !beatAlive(tabs[id], t)) delete tabs[id];
        tabs[tab] = hiddenNow ? { at: t, hidden: true } : t;
        writeObj(LANDED_TABS_KEY, tabs);
        if (t - lastSweep >= LANDED_TAPS_SWEEP_MS) sweep();
    };

    // An older page's single record: split per match, its entries this
    // tab's, once.
    const migrate = () => {
        let raw = null;
        try { raw = storage.getItem(LANDED_TAPS_LEGACY_KEY); } catch (_e) { return; }
        if (raw === null) return;
        const all = parse(raw, {});
        const byKey = new Map();
        for (const id of Object.keys(all)) {
            const r = all[id];
            if (!r || !r.compID || !r.matchID) continue;
            const key = landedTapsKey(r.compID, r.matchID);
            if (!byKey.has(key)) byKey.set(key, readObj(key));
            const { compID: _c, matchID: _m, id: _i, ...entry } = r;
            byKey.get(key)[id] = { ...entry, tab };
        }
        const index = readIndex();
        for (const [key, obj] of byKey) {
            writeObj(key, obj);
            index.add(key);
        }
        writeIndex(index);
        try { storage.removeItem(LANDED_TAPS_LEGACY_KEY); } catch (_e) { /* read again next page */ }
    };

    migrate();
    beat();

    return {
        tab,
        beat,
        sweep,
        /** Records taps of one match as landed. recs: [{ id, where, kind, side, pos, letter }]. */
        note(compID, matchID, recs) {
            if (!recs.length) return;
            const key = landedTapsKey(compID, matchID);
            const obj = readObj(key);
            const t = now();
            const fresh = !Object.keys(obj).length;
            for (const r of recs) {
                if (!r || !r.id) continue;
                const { id, ...entry } = r;
                obj[id] = { ...entry, at: t, tab };
            }
            let ids = Object.keys(obj).filter((id) => t - (Number(obj[id] && obj[id].at) || 0) < LANDED_TAPS_KEEP_MS);
            if (ids.length > LANDED_TAPS_PER_MATCH) {
                ids = ids.sort((a, b) => (obj[b].at || 0) - (obj[a].at || 0)).slice(0, LANDED_TAPS_PER_MATCH);
            }
            const kept = {};
            for (const id of ids) kept[id] = obj[id];
            writeObj(key, kept);
            if (fresh) {
                // A new match record: listed, and the device's whole record
                // swept, so the records of matches never noted again go too.
                const index = readIndex();
                if (!index.has(key)) {
                    index.add(key);
                    writeIndex(index);
                }
                sweep();
            }
        },
        /**
         * The entries of ids this tab tells now, taken out of the match's
         * record: this tab's own, or a gone tab's when this is the live tab
         * with the lowest id. Other tabs' entries stay for them.
         */
        claim(compID, matchID, ids) {
            const key = landedTapsKey(compID, matchID);
            const obj = readObj(key);
            if (!Object.keys(obj).length) return [];
            const live = liveTabs();
            const shown = tellingTabs();
            const told = [];
            for (const id of ids) {
                const rec = obj[id];
                if (!rec || !mayTell(rec, live, shown)) continue;
                told.push({ id, rec });
                delete obj[id];
            }
            if (told.length) writeObj(key, obj);
            return told;
        },
        /** Whether a tap is recorded as landed (for tests). */
        has(compID, matchID, id) {
            return !!readObj(landedTapsKey(compID, matchID))[id];
        },
        /**
         * Sign-out: the record goes with the operator who made the taps, so
         * the next one signed in here is never told of them: this tab's
         * entries, and those of tabs no longer alive (a page reloaded or
         * closed, whose taps this tab would tell). Another live tab's entries
         * stay: its operator is still signed in there (stage 6 review 8, m10).
         * A tap is noted only once it has left the queue, so no queued tap is
         * in the record to keep.
         */
        forget() {
            const live = liveTabs();
            rewriteAll((objs) => {
                const changed = new Set();
                for (const [key, obj] of objs) {
                    for (const id of Object.keys(obj)) {
                        const owner = obj[id] && obj[id].tab;
                        if (!owner || owner === tab || !live.has(owner)) {
                            delete obj[id];
                            changed.add(key);
                        }
                    }
                }
                return changed;
            });
            try { storage.removeItem(LANDED_TAPS_LEGACY_KEY); } catch (_e) { /* nothing more to do */ }
        },
        /** The page goes: its heartbeat with it, so its taps are told by another tab. */
        leave() {
            const tabs = readObj(LANDED_TABS_KEY);
            delete tabs[tab];
            writeObj(LANDED_TABS_KEY, tabs);
        },
    };
}
