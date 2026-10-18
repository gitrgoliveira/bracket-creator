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
//   noted it while that tab is alive (it writes its heartbeat to
//   LANDED_TABS_KEY every LANDED_TAB_BEAT_MS), else the live tab with the
//   lowest id. A tab that tells takes the entry out of the record first, so
//   no later copy tells it again (one notice per tap per device). Nothing
//   waits: a tap no tab may tell yet stays in the record for the next copy.
// - Storage that refuses a write keeps the record in memory for that key;
//   memory is read only while storage still lacks this tab's write, so a
//   record another tab's sign-out removed is never put back.
// - Kept a day, at most LANDED_TAPS_PER_MATCH of a match.

export const LANDED_TAPS_PREFIX = 'bc_landed_taps_v2:';
export const LANDED_TAPS_INDEX = 'bc_landed_taps_v2';
export const LANDED_TAPS_LEGACY_KEY = 'bc_landed_taps_v1';
export const LANDED_TABS_KEY = 'bc_landed_tabs_v1';
export const LANDED_TAPS_KEEP_MS = 24 * 60 * 60 * 1000;
export const LANDED_TAPS_PER_MATCH = 500;
export const LANDED_TAB_BEAT_MS = 20_000;
export const LANDED_TAB_LIVE_MS = 3 * LANDED_TAB_BEAT_MS;

const SEP = '\u001f';

export function landedTapsKey(compID, matchID) {
    return LANDED_TAPS_PREFIX + compID + SEP + matchID;
}

/**
 * @param {{ storage: { getItem(k: string): string|null, setItem(k: string, v: string): void, removeItem(k: string): void },
 *   now?: () => number, tab: string }} opts storage may throw on any call.
 */
export function createLandedTaps({ storage, now = () => Date.now(), tab }) {
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
            return { ...(memory.get(key) || {}) };
        }
        if (raw === null) {
            if (failed.has(key)) return { ...(memory.get(key) || {}) };
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
        } catch (_e) { /* the match keys are still found through memory */ }
    };

    // The tabs alive, by their heartbeat in storage.
    const beat = () => {
        const tabs = readObj(LANDED_TABS_KEY);
        const t = now();
        for (const id of Object.keys(tabs)) if (t - Number(tabs[id]) >= LANDED_TAB_LIVE_MS) delete tabs[id];
        tabs[tab] = t;
        writeObj(LANDED_TABS_KEY, tabs);
    };
    const liveTabs = () => {
        const tabs = readObj(LANDED_TABS_KEY);
        const t = now();
        const live = new Set([tab]);
        for (const id of Object.keys(tabs)) if (t - Number(tabs[id]) < LANDED_TAB_LIVE_MS) live.add(id);
        return live;
    };
    const mayTell = (rec, live) => {
        if (!rec.tab || rec.tab === tab) return true;
        if (live.has(rec.tab)) return false;
        // Its tab is gone: the live tab with the lowest id tells it.
        return [...live].sort()[0] === tab;
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
            byKey.get(key)[id] = { ...r, tab };
        }
        const index = readIndex();
        for (const [key, obj] of byKey) {
            writeObj(key, obj);
            index.add(key);
        }
        writeIndex(index);
        try { storage.removeItem(LANDED_TAPS_LEGACY_KEY); } catch (_e) { /* read again next page */ }
    };

    beat();
    migrate();

    return {
        tab,
        beat,
        /** Records taps of one match as landed. recs: [{ id, where, kind, side, pos, letter }]. */
        note(compID, matchID, recs) {
            if (!recs.length) return;
            const key = landedTapsKey(compID, matchID);
            const obj = readObj(key);
            const t = now();
            const fresh = !Object.keys(obj).length;
            for (const r of recs) if (r && r.id) obj[r.id] = { ...r, compID, matchID, at: t, tab };
            let ids = Object.keys(obj).filter((id) => t - (Number(obj[id] && obj[id].at) || 0) < LANDED_TAPS_KEEP_MS);
            if (ids.length > LANDED_TAPS_PER_MATCH) {
                ids = ids.sort((a, b) => (obj[b].at || 0) - (obj[a].at || 0)).slice(0, LANDED_TAPS_PER_MATCH);
            }
            const kept = {};
            for (const id of ids) kept[id] = obj[id];
            writeObj(key, kept);
            if (fresh) {
                const index = readIndex();
                if (!index.has(key)) {
                    index.add(key);
                    writeIndex(index);
                }
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
            const told = [];
            for (const id of ids) {
                const rec = obj[id];
                if (!rec || !mayTell(rec, live)) continue;
                told.push({ id, rec });
                delete obj[id];
            }
            if (told.length) writeObj(key, obj);
            return told;
        },
        /** Whether a tap is recorded as landed (for tests and the sign-out). */
        has(compID, matchID, id) {
            return !!readObj(landedTapsKey(compID, matchID))[id];
        },
        /**
         * Sign-out: the record goes with the operator who made the taps, but
         * for the taps in keep (still queued, to be sent by this device).
         */
        forgetExcept(keep) {
            const index = readIndex();
            for (const key of index) {
                const obj = readObj(key);
                const kept = {};
                for (const id of Object.keys(obj)) if (keep && keep.has(id)) kept[id] = obj[id];
                writeObj(key, kept);
                if (!Object.keys(kept).length) index.delete(key);
            }
            writeIndex(index);
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
