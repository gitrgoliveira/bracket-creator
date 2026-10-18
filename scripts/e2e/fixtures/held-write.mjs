// Hold a running match's scoring taps in flight, so a race with their echo can
// be observed. Every scoring tap is an event the device queues in its outbox
// and sends to POST /matches/:id/score-events (bc-evts; the taps of one tick
// go as one batch, `{session, events}`). On localhost the answer arrives
// within milliseconds of the batch going out, so a tap made "while the last
// one is out" never lands in that window by itself; the batch is held until
// the test releases it, and it does so only after the next tap has been
// dispatched, as a slow uplink would hold it. The outbox sends a match's
// entries strictly in order, so the next tap's batch goes only once the held
// one is answered. A safety timeout lets go of a hold nobody released, and
// assertNoSafetyRelease() throws when one had to.
const TAP_ROUTE = '**/api/competitions/*/matches/*/score-events';
const isTapBatch = (r) => r.method() === 'POST' && /\/score-events$/.test(new URL(r.url()).pathname);

// Start holding on `page`. Returns:
//   holdNext()  the next tap batch is held; returns { release, sent, answered }
//               where `sent` resolves once that batch has been sent (the tap
//               has left the outbox for the wire), `release()` lets it through
//               and `answered` resolves once its response has reached the page
//   answeredBeforeNextWrite(held)
//               waits for the held batch's response and then for the batch
//               that follows it (up to safetyMs each), and throws unless that
//               batch was sent AFTER the response reached the page
//               (the echo then met a board whose next tap was still unsent,
//               which is the race a journey holds the batch for)
//   bodies      the JSON body of every tap batch the page sent, in send order
//   events      every event those batches carried, in send order (each with
//               its kind, side, letter, target, pos, ...)
//   stop()      release any hold still pending and remove the route and the
//               listener; cleanup only, never throws (call it in a finally,
//               where a throw would replace the body's real assertion error)
//   assertNoSafetyRelease()
//               throws when the safety timeout had to release a hold; call it
//               AFTER the body succeeded, outside the finally
export async function holdScoreWrites(page, { safetyMs = 10_000 } = {}) {
  let hold = null; // armed by holdNext(), taken by the next tap batch
  let safetyFired = 0;
  const bodies = [];
  const events = [];
  const sends = []; // { body, at } in send order, parallel to `bodies`
  const holds = []; // every hold made, taken or not
  const handler = async (route) => {
    const req = route.request();
    if (isTapBatch(req)) {
      let body = null;
      try { body = req.postDataJSON(); } catch { /* not JSON */ }
      bodies.push(body);
      if (body && Array.isArray(body.events)) events.push(...body.events);
      sends.push({ body, at: Date.now() });
      if (hold) {
        const h = hold;
        hold = null;
        h.request = req;
        h.sendIndex = sends.length - 1;
        let timer;
        const timedOut = new Promise((resolve) => {
          timer = setTimeout(() => { if (!h.released) safetyFired += 1; resolve(); }, safetyMs);
        });
        await Promise.race([h.promise, timedOut]);
        clearTimeout(timer);
      }
    }
    await route.continue();
  };
  const onResponse = (res) => {
    const h = holds.find((x) => x.request && x.request === res.request());
    if (h && h.answeredAt === undefined) {
      h.answeredAt = Date.now();
      h.resolveAnswered();
    }
  };
  await page.route(TAP_ROUTE, handler);
  page.on('response', onResponse);
  return {
    bodies,
    events,
    holdNext() {
      const h = { released: false, request: null, sendIndex: -1, answeredAt: undefined };
      h.promise = new Promise((resolve) => { h.release = () => { h.released = true; resolve(); }; });
      h.answered = new Promise((resolve) => { h.resolveAnswered = resolve; });
      holds.push(h);
      hold = h;
      return { release: h.release, sent: page.waitForRequest(isTapBatch), answered: h.answered, hold: h };
    },
    async answeredBeforeNextWrite(held) {
      const h = held.hold;
      const timers = new Set();
      const wait = (ms) => new Promise((resolve) => { const t = setTimeout(resolve, ms); timers.add(t); });
      try {
        let timedOut = false;
        const expire = wait(safetyMs).then(() => { timedOut = true; });
        await Promise.race([h.answered, expire]);
        if (timedOut) throw new Error('held tap batch: its response never reached the page');
        const deadline = Date.now() + safetyMs;
        // The answer is in; the batch that follows it may not have been sent yet.
        while (!sends[h.sendIndex + 1] && Date.now() < deadline) await wait(25);
        const next = sends[h.sendIndex + 1];
        if (!next) throw new Error('held tap batch: the follow-up batch was never sent');
        if (next.at <= h.answeredAt) {
          throw new Error(`held tap batch: the next batch was sent ${h.answeredAt - next.at} ms before the held one's response reached the page, so the race was not exercised`);
        }
      } finally {
        for (const t of timers) clearTimeout(t);
      }
    },
    async stop() {
      // Release first: unrouting does not wait for a handler (Playwright
      // 1.63.0), so a batch still blocked in it would stay held until the
      // safety timer instead of going through now.
      hold = null;
      for (const h of holds) h.release();
      page.off('response', onResponse);
      await page.unroute(TAP_ROUTE, handler);
    },
    assertNoSafetyRelease() {
      if (safetyFired) throw new Error(`held tap batch: the ${safetyMs} ms safety timeout released ${safetyFired} hold(s) nobody released`);
    },
  };
}
