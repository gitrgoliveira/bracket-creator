// base_sender_lease.jsx: the ONE owner of which tab of a device is sending a
// base of the shared outbox (a match, or a `lineup:`, `announce:` ... base). A
// leaf: it reads and writes only the storage it is handed.
//
// Every tab of a device flushes the one stored outbox, so with two tabs open
// both sent an entry (a court call POST and its withdrawal DELETE twice), and an
// entry one tab took up mid-pass could go after a later entry of the same match
// the other tab had already sent. A tab now sends a base only while it holds
// that base's lease:
//
// - One record in storage PER BASE, `BASE_LEASE_PREFIX + base`: { tab, at }. A
//   tab takes a base's lease for the turn it is about to send and gives it up
//   when the turn ends (`release`), so the outbox is never owned by a tab for
//   longer than a send. The record is the base's own, so two tabs working on
//   different bases never write the same item and cannot overwrite each other.
// - A lease is alive for BASE_LEASE_LIVE_MS (a few seconds) after its last beat. The holder
//   beats every BASE_LEASE_BEAT_MS while it holds one and renews before each
//   entry it sends (`renew`), so a tab that crashes mid-send gives the base up
//   once its lease is old, and a tab that is closed gives its leases up at once
//   (`leave`, on pagehide; a browser that closes a page without it leaves its
//   record, which the next claim of that base takes over once it is old).
//   Nothing of a tab that is gone stays unsent: another tab takes the base
//   over and sends it.
// - A tab that finds a base leased by another live tab does not send it. It
//   asks again on the flush's backoff, and at once when the holder's turn ended
//   with everything sent (`release(base, true)` writes BASE_LEASE_WAKE_KEY, which
//   the other tabs read as a storage event). A turn that ended failing wakes
//   nobody: two tabs would otherwise hand a failing base to each other as fast
//   as storage events travel, with no backoff.
// - Storage that refuses a write gives no lease: the tab sends as it always
//   did, which is safe because the server answers a resend as recorded.
//
// Residual: two tabs claiming one base within the same storage sync, or a
// holder whose beat a hidden page slowed past the lease's life while one request
// is out (Chrome runs the timers of a page hidden for five minutes about once a
// minute, and a request may take its 12 s deadline), both send, a resend the
// server answers as recorded, never a loss. Between two entries the holder
// renews, so only the request under way can outlive its lease; a holder whose
// lease another tab took over meanwhile goes on with the rest of its turn
// beside the new holder, the same harmless resend.

export const BASE_LEASE_PREFIX = 'bc_base_senders_v1:';
export const BASE_LEASE_WAKE_KEY = 'bc_base_senders_wake_v1';
export const baseLeaseKey = (base) => BASE_LEASE_PREFIX + base;
// Short, so a tab closed or crashed mid-send holds nobody up for long. A visible
// holder beats well inside it; a hidden page's timers can be slowed far past it,
// which is why the holder also renews before each entry (`renew`).
export const BASE_LEASE_LIVE_MS = 6_000;
export const BASE_LEASE_BEAT_MS = 2_000;

function parse(raw) {
    if (!raw) return null;
    try {
        const v = JSON.parse(raw);
        return v && typeof v === 'object' && typeof v.tab === 'string' ? v : null;
    } catch (_e) {
        return null;
    }
}

/**
 * @param {{ storage: { getItem(k: string): string|null, setItem(k: string, v: string): void, removeItem(k: string): void },
 *   tab: string, now?: () => number }} opts storage may throw on any call.
 */
export function createBaseLease({ storage, tab, now = () => Date.now() }) {
    const mine = new Set();
    let timer = null;
    let wakes = 0;

    const read = (base) => {
        try {
            return parse(storage.getItem(baseLeaseKey(base)));
        } catch (_e) {
            return null;
        }
    };
    const write = (base, record) => {
        try {
            storage.setItem(baseLeaseKey(base), JSON.stringify(record));
            return true;
        } catch (_e) {
            return false;
        }
    };
    const alive = (record, t) => !!record && t - Number(record.at) < BASE_LEASE_LIVE_MS;
    const stopBeat = () => {
        if (timer !== null) { clearInterval(timer); timer = null; }
    };
    const beat = () => {
        const t = now();
        // A holder whose beat was late (a slow request, a throttled page) still
        // owns the lease while no other tab has claimed it.
        for (const base of mine) {
            const held = read(base);
            if (held && held.tab === tab) write(base, { tab, at: t });
        }
    };
    const startBeat = () => {
        if (timer !== null || typeof setInterval !== 'function') return;
        timer = setInterval(beat, BASE_LEASE_BEAT_MS);
        if (timer && typeof timer.unref === 'function') timer.unref();
    };

    return {
        tab,
        /**
         * Takes the base's lease for this tab. True when this tab holds it now (it
         * did, or it was free, or its holder's lease is old); false when another
         * live tab holds it, or one claimed it as this tab did.
         */
        claim(base) {
            const t = now();
            const held = read(base);
            if (alive(held, t) && held.tab !== tab) return false;
            if (!write(base, { tab, at: t })) return true;
            const back = read(base);
            if (!back || back.tab !== tab) return false;
            mine.add(base);
            startBeat();
            return true;
        },
        /**
         * Gives the base up, if this tab holds it. `wake`: the turn sent all it
         * held, so a tab that was turned away asks again now.
         */
        release(base, wake = false) {
            if (!mine.delete(base)) return;
            const held = read(base);
            if (held && held.tab === tab) {
                try {
                    storage.removeItem(baseLeaseKey(base));
                } catch (_e) { /* an old record is taken over once its life is up */ }
            }
            if (mine.size === 0) stopBeat();
            if (wake) {
                try {
                    storage.setItem(BASE_LEASE_WAKE_KEY, `${tab}:${++wakes}`);
                } catch (_e) { /* a tab that is not woken asks on its backoff */ }
            }
        },
        /** Renews every lease this tab holds, now: the timer's beat, asked for. */
        renew: beat,
        /** The page goes: every lease it holds goes with it. */
        leave() {
            for (const base of mine) this.release(base);
        },
        /**
         * How long until the lease another tab holds on the base runs out unless
         * that tab beats again; 0 when none is held.
         */
        expiresIn(base) {
            const held = read(base);
            return held ? Math.max(0, BASE_LEASE_LIVE_MS - (now() - Number(held.at))) : 0;
        },
        /** The bases this tab holds (the tests read it). */
        held() {
            return [...mine];
        },
    };
}
