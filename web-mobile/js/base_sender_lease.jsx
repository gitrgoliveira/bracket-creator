// base_sender_lease.jsx: the ONE owner of which tab of a device is sending a
// base of the shared outbox (a match, or a `lineup:`, `announce:` ... base). A
// leaf: it reads and writes only the storage it is handed.
//
// Every tab of a device flushes the one stored outbox, so with two tabs open
// both sent an entry (a court call POST and its withdrawal DELETE twice), and an
// entry one tab took up mid-pass could go after a later entry of the same match
// the other tab had already sent. A tab now sends a base only while it holds
// that base's lease. A lease is held one of two ways, chosen ONCE when the lease
// is built (`createBaseLease`, by feature detection, never by user agent):
//
// - Web Locks (`navigator.locks`, `createLockLease`): the lock named for the base
//   is held while the tab sends it. The browser itself gives a lock up when its
//   tab closes or crashes, so the next tab takes the base over at once, and a
//   tab that was turned away is told when the lock frees (`whenFree`). A turn
//   that ended failing first takes the base's cooling lock for a lease life, and
//   `whenFree` waits for that one too, so it wakes nobody (the rule below).
// - The timed note, below, where the browser or the page has no Web Locks (an
//   older browser or build, or a page served over plain http, where browsers
//   hide `navigator.locks`: a phone reaching the app by its LAN address).
//
// The timed note is a FALLBACK kept on purpose: the operator asked for Web Locks
// with the note as the path for a browser without them (decision L1), which is
// the one place this app keeps a fallback beside the better mechanism. Do not
// copy it elsewhere, and do not drop it without that ruling.
//
// The timed note:
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
const baseLockName = (base) => 'bc_base_sender_v1:' + base;
const baseCoolName = (base) => 'bc_base_sender_cool_v1:' + base;

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
 * The lease of one tab over the bases it sends, behind one interface:
 * `claim(base)` a promise of whether this tab holds the base now; `release(base,
 * wake)`; `renew()`; `leave()`; `whenFree(base)` a promise that settles when
 * another tab's hold on the base is over; `held()`.
 *
 * @param {{ storage: { getItem(k: string): string|null, setItem(k: string, v: string): void, removeItem(k: string): void },
 *   tab: string, now?: () => number, locks?: object|null }} opts storage may throw
 *   on any call. `locks` is `detectWebLocks()`'s answer (or a stand-in): given,
 *   the lease is held with Web Locks and the storage is not used.
 */
export function createBaseLease({ storage, tab, now = () => Date.now(), locks = null }) {
    return locks ? createLockLease({ locks, tab }) : createNoteLease({ storage, tab, now });
}

/** The page's `navigator.locks` when it has Web Locks, else null. Asked once, where the lease is built. */
export function detectWebLocks() {
    const locks = globalThis.navigator && globalThis.navigator.locks;
    return locks && typeof locks.request === 'function' ? locks : null;
}

// The lease held with Web Locks. A request that finds the lock taken is
// answered `false` at once (`ifAvailable`), so a tab never queues behind a
// sender's turn; the granted callback returns a promise that `give` settles to
// let the lock go.
function createLockLease({ locks, tab }) {
    const holds = new Map();    // base -> { give, done }: the locks this tab holds
    const ending = new Map();   // base -> promise: a release this tab made that the lock manager has not finished
    // A turn that ended failing takes the base's cooling lock, and lets the base
    // go only once it holds it, so a tab queued in `whenFree` waits a lease life
    // more, as the timed note's waiter does. A cooling lock already taken (an
    // earlier failure, here or in another tab) is cooling the base already.
    const cool = (base, give) => {
        try {
            locks.request(baseCoolName(base), { ifAvailable: true }, (lock) => {
                give();
                return lock ? new Promise((done) => { setTimeout(done, BASE_LEASE_LIVE_MS); }) : undefined;
            }).catch(give);
        } catch (_e) {
            give();
        }
    };
    const release = (base, wake = false) => {
        const hold = holds.get(base);
        if (!hold) return;
        holds.delete(base);
        const gone = hold.done.then(() => { if (ending.get(base) === gone) ending.delete(base); }, () => {});
        ending.set(base, gone);
        if (wake) hold.give();
        else cool(base, hold.give);
    };
    return {
        tab,
        claim(base) {
            if (holds.has(base)) return Promise.resolve(true);
            // A claim right after this tab's own release waits for the release to land,
            // or the lock would still look taken, by this tab itself.
            return Promise.resolve(ending.get(base)).then(() => new Promise((resolve) => {
                try {
                    const done = locks.request(baseLockName(base), { ifAvailable: true }, (lock) => {
                        if (!lock) { resolve(false); return undefined; }
                        return new Promise((give) => { holds.set(base, { give, done }); resolve(true); });
                    });
                    // A page that refuses locks (an opaque origin) gives none: the tab
                    // sends as it always did, safe because the server answers a resend as recorded.
                    done.catch(() => resolve(true));
                } catch (_e) {
                    resolve(true);
                }
            }));
        },
        // Letting the lock go is the wake: a tab queued in `whenFree` is granted it,
        // at once when the turn sent all it held (`wake`), after the cooling otherwise.
        release,
        // A held lock needs no beat.
        renew() {},
        // The page goes: its bases go to the other tabs at once, as a closed tab's do.
        leave() {
            for (const base of Array.from(holds.keys())) release(base, true);
        },
        whenFree(base) {
            return new Promise((resolve) => {
                try {
                    // Queued behind the holder, then behind its cooling; each granted, and
                    // given straight back, once it is over. Settled only when the lock is
                    // back, or a claim made at once finds it taken.
                    locks.request(baseLockName(base), () => undefined)
                        .then(() => locks.request(baseCoolName(base), () => undefined))
                        .then(() => resolve(), () => resolve());
                } catch (_e) {
                    resolve();
                }
            });
        },
        held() {
            return [...holds.keys()];
        },
    };
}

// The lease held with the timed note (see the header).
function createNoteLease({ storage, tab, now }) {
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
         * Takes the base's lease for this tab. Resolves true when this tab holds it now (it
         * did, or it was free, or its holder's lease is old); false when another
         * live tab holds it, or one claimed it as this tab did.
         */
        async claim(base) {
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
         * Settles once the lease another tab holds on the base would have run out
         * unless that tab beats again (at once when none is held). A holder that is
         * alive beats and the next claim finds it held again.
         */
        whenFree(base) {
            const held = read(base);
            const left = held ? Math.max(0, BASE_LEASE_LIVE_MS - (now() - Number(held.at))) : 0;
            return new Promise((resolve) => { setTimeout(resolve, left + 50); });
        },
        /** The bases this tab holds (the tests read it). */
        held() {
            return [...mine];
        },
    };
}
