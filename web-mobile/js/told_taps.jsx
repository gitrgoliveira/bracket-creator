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
//   device: the tab whose answer it is tells the operator, and every other tab
//   hears of it through the storage event (`onStored`) and takes its own note
//   down, so no tab keeps a stale "Not applied".
// - `settle` forgets a told tap whose answer confirmed it was not applied.
// - Kept a day at most (`TOLD_TAPS_KEEP_MS`); a sign-out forgets this tab's.
// Storage that refuses a write keeps the record in memory for that key.

export const TOLD_TAPS_PREFIX = 'bc_told_taps_v1:';
export const TOLD_TAPS_KEEP_MS = 24 * 60 * 60 * 1000;
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
            return { ...(memory.get(key) || {}) };
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

    return {
        /** Notes a tap told as not applied; false when the device already told it. */
        tell(compID, matchID, id) {
            if (!id) return false;
            const key = toldTapsKey(compID, matchID);
            const obj = read(key);
            if (obj[id]) return false;
            obj[id] = { at: now(), tab };
            write(key, obj);
            return true;
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
