// told_taps.jsx: the ONE owner of which pending taps a board has told the
// operator were not applied (stage 6 review 8, m12; shared by every tab since
// review 9, F4). A leaf: it reads and writes only the storage it is handed.
//
// A board judges a pending tap from the copy of the match it holds, which can
// be behind the server: a tap on a bout shown removed is told "Not applied" at
// once (tellPendingTapRefused). Every tab of the device sends the shared
// outbox, so the tap's answer can reach another tab than the one whose board
// told it. The record is therefore kept in browser storage, one small record
// per match (TOLD_TAPS_PREFIX + match), each entry naming the tab that told
// it:
// - `tell` notes a tap told; it answers false when the device already told it
//   (any tab), so the operator's alert is raised once per device.
// - `takeBack` marks a told tap the server applied (`back`), once for the
//   device, and every other tab hears of it through the storage event
//   (`onStored`) and takes its own note down, so no tab keeps a stale "Not
//   applied". The operator is told "applied after all" once per device
//   (`claimAlert`), by the tab that told it (`tellerOf`) or, when that tab is
//   hidden or gone, by a shown tab (landed_taps.jsx `tellsFor`; stage 6
//   review 10, m1).
// - `settle` forgets a told tap whose answer confirmed it was not applied;
//   the other tabs hear of every entry that goes (`goneOnStored`).
// - Kept a day at most (`TOLD_TAPS_KEEP_MS`) and at most `TOLD_TAPS_MAX` in
//   all (`sweep`, `beat`); a sign-out forgets this tab's.
// Storage that refuses a write keeps the record in memory for that key.

export const TOLD_TAPS_PREFIX = 'bc_told_taps_v1:';
export const TOLD_TAPS_KEEP_MS = 24 * 60 * 60 * 1000;
// The device keeps at most this many entries in all, the latest, and sweeps
// every record (not only one written again) as a record is made and at most
// every TOLD_TAPS_SWEEP_MS from the heartbeat, so the record has a fixed
// ceiling beside the outbox in the same storage (stage 6 review 10, m7: a
// `back` entry, and every entry of a match never written again, stayed for
// good).
export const TOLD_TAPS_MAX = 500;
export const TOLD_TAPS_SWEEP_MS = 5 * 60 * 1000;
const SEP = '\u001f';

export function toldTapsKey(compID, matchID) {
    return TOLD_TAPS_PREFIX + compID + SEP + matchID;
}

function parseKey(key) {
    if (typeof key !== 'string' || !key.startsWith(TOLD_TAPS_PREFIX)) return null;
    const rest = key.slice(TOLD_TAPS_PREFIX.length);
    const i = rest.indexOf(SEP);
    if (i < 0) return null;
    return { compID: rest.slice(0, i), matchID: rest.slice(i + 1) };
}

function parse(raw) {
    if (raw === null || raw === undefined) return {};
    try {
        const v = JSON.parse(raw);
        return v && typeof v === 'object' && !Array.isArray(v) ? v : {};
    } catch (_e) {
        return {};
    }
}

/**
 * @param {{ storage: { getItem(k: string): string|null, setItem(k: string, v: string): void,
 *   removeItem(k: string): void, keys?(): string[] }, now?: () => number, tab: string }} opts
 */
export function createToldTaps({ storage, now = () => Date.now(), tab }) {
    const memory = new Map();
    const read = (key) => {
        let raw;
        try {
            raw = storage.getItem(key);
        } catch (_e) {
            return { ...memory.get(key) };
        }
        if (raw === null && memory.has(key)) return { ...memory.get(key) };
        return parse(raw);
    };
    const write = (key, obj) => {
        const t = now();
        for (const id of Object.keys(obj)) {
            if (t - (Number(obj[id] && obj[id].at) || 0) >= TOLD_TAPS_KEEP_MS) delete obj[id];
        }
        try {
            if (Object.keys(obj).length === 0) storage.removeItem(key);
            else storage.setItem(key, JSON.stringify(obj));
            memory.delete(key);
        } catch (_e) {
            memory.set(key, obj);
        }
    };
    const keys = () => {
        const out = new Set(memory.keys());
        if (typeof storage.keys === 'function') {
            try {
                for (const k of storage.keys()) if (typeof k === 'string' && k.startsWith(TOLD_TAPS_PREFIX)) out.add(k);
            } catch (_e) { /* memory alone */ }
        }
        return out;
    };

    // The sweep: entries older than TOLD_TAPS_KEEP_MS go from EVERY record,
    // and the device keeps at most TOLD_TAPS_MAX in all, the latest. Answers
    // the ids it took out, for the tab to forget what it holds of them.
    let lastSweep = -Infinity;
    const sweep = () => {
        const t = now();
        lastSweep = t;
        const objs = new Map();
        const kept = [];
        for (const key of keys()) {
            const obj = read(key);
            objs.set(key, obj);
            for (const id of Object.keys(obj)) kept.push([Number(obj[id] && obj[id].at) || 0, key, id]);
        }
        const removed = [];
        const changed = new Set();
        kept.sort((a, b) => a[0] - b[0]);
        const over = Math.max(0, kept.length - TOLD_TAPS_MAX);
        kept.forEach(([at, key, id], i) => {
            if (i < over || t - at >= TOLD_TAPS_KEEP_MS) {
                delete objs.get(key)[id];
                changed.add(key);
                removed.push(id);
            }
        });
        for (const key of changed) {
            // Read again just before the write: an entry another tab wrote
            // meanwhile stays.
            const fresh = read(key);
            const left = objs.get(key);
            for (const id of Object.keys(fresh)) {
                if (!(id in left) && removed.includes(id)) delete fresh[id];
            }
            write(key, fresh);
        }
        return removed;
    };

    return {
        sweep,
        /** The heartbeat: sweeps when one is due; answers the ids taken out. */
        beat() {
            return now() - lastSweep >= TOLD_TAPS_SWEEP_MS ? sweep() : [];
        },
        /** Notes a tap told as not applied; false when the device already told it. */
        tell(compID, matchID, id) {
            if (!id) return false;
            const key = toldTapsKey(compID, matchID);
            const obj = read(key);
            if (obj[id]) return false;
            const fresh = !Object.keys(obj).length;
            obj[id] = { at: now(), tab };
            write(key, obj);
            if (fresh) sweep();
            return true;
        },
        /** The tab that told the tap, or "" when the device has no record of it. */
        tellerOf(compID, matchID, id) {
            const rec = read(toldTapsKey(compID, matchID))[id];
            return (rec && rec.tab) || '';
        },
        /**
         * The "applied after all" alert of a tap taken back: true once for the
         * device, to the tab that raises it.
         */
        claimAlert(compID, matchID, id) {
            const key = toldTapsKey(compID, matchID);
            const obj = read(key);
            const rec = obj[id];
            if (!rec || !rec.back || rec.alerted) return false;
            // A marker naming this tab, read back before the alert is raised
            // (stage 6 review 11, B): a tab whose copy was behind and wrote its
            // own marker meanwhile raises it instead, never both. Residual as
            // the landed-tap claim's.
            obj[id] = { ...rec, alerted: tab };
            write(key, obj);
            const back = read(key)[id];
            return !!back && back.alerted === tab;
        },
        /** Whether the device told the tap and has not taken it back. */
        told(compID, matchID, id) {
            const rec = read(toldTapsKey(compID, matchID))[id];
            return !!rec && !rec.back;
        },
        /** Marks a told tap applied after all; true once for the device. */
        takeBack(compID, matchID, id) {
            const key = toldTapsKey(compID, matchID);
            const obj = read(key);
            const rec = obj[id];
            if (!rec || rec.back) return false;
            obj[id] = { ...rec, back: true, at: now() };
            write(key, obj);
            return true;
        },
        /** A told tap whose answer confirmed it: nothing more to tell. */
        settle(compID, matchID, id) {
            const key = toldTapsKey(compID, matchID);
            const obj = read(key);
            if (!obj[id]) return;
            delete obj[id];
            write(key, obj);
        },
        /**
         * Another tab changed a record: the taps it took back, for this tab
         * to take its own notes down.
         */
        onStored(key, oldValue, newValue) {
            const at = parseKey(key);
            if (!at) return [];
            const was = parse(oldValue);
            const is = parse(newValue);
            const out = [];
            for (const id of Object.keys(is)) {
                if (is[id] && is[id].back && !(was[id] && was[id].back)) out.push({ ...at, id });
            }
            return out;
        },
        /**
         * Another tab changed a record: the taps whose entries it took out
         * (settled, swept, forgotten), for this tab to forget what it holds of
         * them.
         */
        goneOnStored(key, oldValue, newValue) {
            const at = parseKey(key);
            if (!at) return [];
            const was = parse(oldValue);
            const is = parse(newValue);
            return Object.keys(was).filter((id) => !(id in is)).map((id) => ({ ...at, id }));
        },
        /** Keeps only this tab's entries whose taps `keep` names (a revoked password's park). */
        prune(keep) {
            for (const key of keys()) {
                const obj = read(key);
                let changed = false;
                for (const id of Object.keys(obj)) {
                    if (obj[id] && obj[id].tab === tab && !obj[id].back && !keep(id)) {
                        delete obj[id];
                        changed = true;
                    }
                }
                if (changed) write(key, obj);
            }
        },
        /** Sign-out: this tab's entries go with the operator who was told. */
        forget() {
            for (const key of keys()) {
                const obj = read(key);
                let changed = false;
                for (const id of Object.keys(obj)) {
                    if (!obj[id] || obj[id].tab === tab) {
                        delete obj[id];
                        changed = true;
                    }
                }
                if (changed) write(key, obj);
            }
            memory.clear();
        },
    };
}
