// The refetch gate (bc-sseq FR4): the ONE owner of "does this event need a
// refetch", made by each host that fetches again (app.jsx, admin.jsx,
// admin_shiaijo.jsx). A leaf module beside read_seq.jsx, which names the seq a
// read carries; patch.jsx applies pushed patches and does not know it.

import { readSeqOf } from './read_seq.jsx';

// HELD_PUSH_WAIT_MS: no refetch the gate schedules is sent sooner than this
// after the event that asked for it. The server holds a running match's push
// for up to its coalescing window (matchCoalesceWindow, 250 ms,
// internal/mobileapp/broadcast_coalescer.go) and a held push takes its seq
// only when it is sent, so a read made before it is out names a lower seq
// and the push would ask for a second fetch. The window plus 50 ms lets that
// push go first, so the read's seq covers it. Pinned to the Go constant by
// the shared table internal/mobileapp/testdata/held_push_wait.json (the Go
// half is held_push_wait_test.go, the JS half refetch_gate.test.jsx).
const HELD_PUSH_WAIT_MS = 300;

// FETCH_DEADLINE_MS: a gate fetch with no answer after this is abandoned as
// failed, so a fetch that never settles cannot freeze the gate. 30 s is
// longer than a slow tablet on a bad link takes to answer, and short enough
// that a later event recovers.
const FETCH_DEADLINE_MS = 30000;

// createRefetchGate (bc-sseq FR4) is the ONE owner of "does this event need
// a refetch". A host makes one per thing it fetches again (a competition's
// detail, the list, the court feed) and sends every event that would have
// refetched through `event(seq)`, instead of scheduling a fetch per event.
//   - schedule(run, delayMs): the host's timer pool (timerPool.schedule),
//     cleared with the host's effect;
//   - fetch(): sends the read and answers a promise of the value the fetcher
//     returned, whose seq read_seq.jsx holds (readSeqOf);
//   - take(): called as the fetch is sent; answers apply(value), which takes
//     the fetch's answer (the push ledger's keeper is made at send time), or
//     nothing to drop it. apply returns exactly false when it dropped the
//     answer unshown (the host's audience changed while it was out): its seq
//     then covers nothing. Any other return means it was applied;
//   - maxDelayMs: the top of the host's jitter window. Each fetch is sent a
//     delay drawn uniformly from HELD_PUSH_WAIT_MS to it (the wait is the
//     window's start, never added before it);
//   - onError(err): a failed fetch or apply (console.error by default).
// The life of a refetch:
//   - asked for: by an event above the applied seq, or, forced, by an event
//     with no seq, now(seq?) (reconnect, tab resume, a gap; a seq it names
//     counts as seen) or restart(). An ask while one is scheduled joins it;
//     an ask while one is out asks for one more after it, except now() and
//     restart(), which let the one out go (its answer is ignored, as after
//     the deadline) and schedule a new one, since it may be hung on the
//     connection they resync.
//   - sent: after the wait, unless it is not forced and an answer applied
//     meanwhile (an out-of-gate read, such as a save's own refresh) covers
//     every event seen.
//   - answered: an answer naming a lower seq than the applied one is late (a
//     build that began earlier, openapi EventSeq): it is not applied, so it
//     never puts older data over a newer read, and the applied seq only
//     rises (the first answer after seed() is never late: it replaces the
//     seed, so it is applied whatever its seq). Any answer asks for one more when an event seen while it was out
//     is above its seq (a missing header covers nothing, an event with no seq
//     is never covered), or, once only, when its seq is below an event seen
//     before it was sent (it joined a build the server elected earlier).
//   - failed: a fetch that rejects, an apply that throws, or a fetch with no
//     answer after FETCH_DEADLINE_MS (abandoned: its late answer is ignored)
//     is asked for again once, as forced as the one that failed; an event
//     seen while it was out still gets its refetch. A second failure waits
//     for the next event or now(): an offline device must not poll.
//   - ended: forget() (resync_required) drops every seq: nothing counts as
//     applied, and an answer to a fetch sent before it is applied but not
//     adopted. restart() (the stream reopened; a seq cannot be compared
//     across a server restart) is forget() and a forced ask, which lets a
//     fetch out go. dispose() (the
//     host's effect ended): a fetch still out is neither applied nor followed
//     by another, and nothing is sent again.
// A host made after its page read seeds the gate with that read's seq:
// seed(seq), whose first answer replaces it (the seed may be older than a
// server restart). A fetch that applies the same state outside the gate (a first load, a
// refresh after a save) reports the seq of the value its fetcher returned,
// before any merge: `applied(readSeqOf(fetched))`. event() answers true when
// the event asked for (or joined) a refetch, false when an applied answer
// already covers it.
function createRefetchGate({ schedule, fetch, take, maxDelayMs = HELD_PUSH_WAIT_MS, random = Math.random, onError } = {}) {
    const report = typeof onError === 'function' ? onError : (err) => console.error('Refresh failed:', err);
    const top = Math.max(HELD_PUSH_WAIT_MS, Number(maxDelayMs) || 0);
    let applied = 0;      // seq of the newest answer applied (0: none)
    let seen = 0;         // highest event seq seen (events and now(seq), never an answer)
    let generation = 0;   // forget()s so far: an answer sent before one is not adopted
    let failed = false;   // the last fetch failed and has been asked for again
    let disposed = false; // the host's effect ended: nothing is sent or applied
    let seeded = false;   // applied came from seed(): the next answer replaces it
    let flights = 0;      // flight ids handed out
    // flight: the fetch out, or null. id: its number; gen: the generation it
    // was sent in; sent: `seen` when it was sent; seq: highest event seq seen
    // while out; unknown: an event with no seq arrived while out; recheck: it
    // repeats one that answered behind; forced: it was forced, so its retry
    // is too.
    let flight = null;
    // wanted: the next refetch. scheduled: its timer is set; again: asked for
    // while one was out; forced: applied() cannot cancel it; recheck: it is
    // the repeat of an answer that came back behind.
    const wanted = { scheduled: false, again: false, forced: false, recheck: false };

    const isSeq = (seq) => typeof seq === 'number' && Number.isSafeInteger(seq) && seq > 0;

    // adopt records an answer's seq. It only ever rises: a lower one is a
    // late answer (a build that began earlier) and holds nothing new. The
    // exception is the first answer after seed(): the seed came from a read
    // made before the stream may have restarted, so that answer replaces it.
    const adopt = (seq) => {
        if (!isSeq(seq)) return;
        if (seeded) {
            seeded = false;
            applied = seq;
        } else if (seq > applied) applied = seq;
    };

    // note records an event's seq as seen and, while a fetch is out, as seen
    // during it: settle() judges it once the answer lands.
    const note = (seq) => {
        if (!isSeq(seq)) {
            if (flight) flight.unknown = true;
            return;
        }
        if (seq > seen) seen = seq;
        if (flight && seq > flight.seq) flight.seq = seq;
    };

    // ask wants a refetch. A forced one (now(), an event with no seq, a failed
    // fetch) is sent whatever has been applied since. now() and restart()
    // ask while a fetch is out only to resync a connection that may have
    // dropped it, so the fetch out is let go (its answer, if one comes, is
    // ignored) and a new one is sent, never waited for behind it.
    const ask = (force) => {
        if (disposed) return;
        if (force) wanted.forced = true;
        if (wanted.scheduled) return;
        if (flight && force) {
            flight = null;
            wanted.again = false;
        }
        if (flight) { wanted.again = true; return; }
        wanted.scheduled = true;
        schedule(fire, HELD_PUSH_WAIT_MS + random() * (top - HELD_PUSH_WAIT_MS));
    };

    // settle ends flight `f`: answered with `seq` when ok, failed otherwise.
    const settle = (f, seq, ok) => {
        flight = null;
        let more = wanted.again;
        let force = false;
        wanted.again = false;
        const current = f.gen === generation;
        if (current && ok) {
            if (f.unknown) more = force = true;
            else if (f.seq > 0 && !(isSeq(seq) && seq >= f.seq)) more = true;
            // Behind an event seen before it was sent: once only, so a server
            // whose seq restarted below the page's cannot loop it.
            else if (!f.recheck && isSeq(seq) && seq < f.sent) more = wanted.recheck = true;
            adopt(seq);
            failed = false;
        } else if (f.unknown || f.seq > 0) {
            more = force = true;
        }
        // A failure is asked for again once, as forced as the fetch it repeats
        // (else fire() would find nothing to ask after now() or restart()).
        if (current && !ok && !failed) {
            failed = more = true;
            force = force || f.forced;
        } else if (!ok) failed = false;
        if (more) ask(force);
    };

    // out: flight `id` is still the one the gate waits for.
    const out = (id) => !disposed && flight !== null && flight.id === id;

    function fire() {
        wanted.scheduled = false;
        if (disposed) return;
        if (flight) { wanted.again = true; return; }
        const recheck = wanted.recheck;
        wanted.recheck = false;
        if (!wanted.forced && seen <= applied) return;
        flights += 1;
        const f = { id: flights, gen: generation, sent: seen, seq: 0, unknown: false, recheck, forced: wanted.forced };
        wanted.forced = false;
        flight = f;
        let apply;
        let sent;
        try {
            apply = take ? take() : undefined;
            sent = Promise.resolve(fetch());
        } catch (err) {
            sent = Promise.reject(err);
        }
        schedule(() => {
            if (!out(f.id)) return;
            report(new Error(`refetch did not answer in time (${FETCH_DEADLINE_MS} ms)`));
            settle(f, 0, false);
        }, FETCH_DEADLINE_MS);
        sent.then((value) => {
            if (!out(f.id)) return;
            const seq = readSeqOf(value);
            let ok = true;
            let kept = true;
            try {
                // Older than a read already shown: applying it would put
                // older data back. A seed is no read shown by this gate, so
                // the first answer is never late against it.
                if (typeof apply === 'function' && (seeded || !(isSeq(seq) && seq < applied))) kept = apply(value) !== false;
            } catch (err) {
                ok = false;
                report(err);
            }
            settle(f, kept ? seq : 0, ok);
        }, (err) => {
            if (!out(f.id)) return;
            report(err);
            settle(f, 0, false);
        });
    }

    const forget = () => {
        generation += 1;
        applied = 0;
        seen = 0;
        seeded = false;
    };

    return {
        event(seq) {
            if (isSeq(seq) && seq <= applied) return false;
            note(seq);
            if (!flight) ask(!isSeq(seq));
            return true;
        },
        now(seq) {
            if (isSeq(seq)) note(seq);
            ask(true);
        },
        applied(seq) { adopt(seq); },
        // late(seq): whether an answer read at `seq`, made outside the gate,
        // is older than one the gate applied, the rule its own answers keep
        // (a seed is no answer shown, so nothing is late against it). A host
        // drops such an answer before showing it: shown, it would put older
        // data back while the applied seq stayed above it.
        late(seq) { return !seeded && isSeq(seq) && seq < applied; },
        // seed(seq): a gate made after the page read starts from that read's
        // seq, so its first event does not fetch what the page holds. The
        // seed may be older than a server restart, so the first answer that
        // follows replaces it instead of being held at or below it.
        seed(seq) {
            adopt(seq);
            seeded = isSeq(seq);
        },
        forget,
        restart() {
            forget();
            ask(true);
        },
        // dispose() ends the gate with the host's effect (call it in the
        // effect's cleanup): the seq it held stays readable through
        // appliedSeq().
        dispose() { disposed = true; },
        // appliedSeq: the seq the gate counts as applied (tests and logs).
        appliedSeq() { return applied; },
        // pending: a refetch is scheduled, asked for again, or out. A host
        // that ends a gate read this first, to hand the read on to the next.
        pending() { return wanted.scheduled || wanted.again || flight !== null; },
    };
}

export { createRefetchGate, HELD_PUSH_WAIT_MS, FETCH_DEADLINE_MS };
