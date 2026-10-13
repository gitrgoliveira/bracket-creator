// Hold a running match's score write in flight, so a race with its echo can be
// observed. On localhost the answer to an autosave arrives within milliseconds
// of the save going out, so a tap made "while the save is out" never lands in
// that window by itself; the write is held until the test releases it, and it
// does so only after the next tap has been dispatched, as a slow uplink would
// hold it. A safety timeout lets go of a hold nobody released, and
// assertNoSafetyRelease() throws when one had to.
const SCORE_ROUTE = '**/api/competitions/*/matches/*/score';
const isScorePut = (r) => r.method() === 'PUT' && /\/score$/.test(r.url());

// Start holding on `page`. Returns:
//   holdNext()  the next score PUT is held; returns { release, sent, answered }
//               where `sent` resolves once that PUT has been sent (so the
//               autosave debounce has run), `release()` lets it through and
//               `answered` resolves once its response has reached the page
//   answeredBeforeNextWrite(held)
//               waits for the held write's response, then throws unless it
//               reached the page BEFORE the write that followed it was sent
//               (the echo then met a board whose next edit was still unsent,
//               which is the race a journey holds the write for)
//   bodies      the JSON body of every score PUT the page sent, in send order
//   stop()      release any hold still pending and remove the route and the
//               listener; cleanup only, never throws (call it in a finally,
//               where a throw would replace the body's real assertion error)
//   assertNoSafetyRelease()
//               throws when the safety timeout had to release a hold; call it
//               AFTER the body succeeded, outside the finally
export async function holdScoreWrites(page, { safetyMs = 10_000 } = {}) {
  let hold = null; // armed by holdNext(), taken by the next PUT
  let safetyFired = 0;
  const bodies = [];
  const sends = []; // { body, at } in send order, parallel to `bodies`
  const holds = []; // every hold made, taken or not
  const handler = async (route) => {
    const req = route.request();
    if (isScorePut(req)) {
      let body = null;
      try { body = req.postDataJSON(); } catch { /* not JSON */ }
      bodies.push(body);
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
  await page.route(SCORE_ROUTE, handler);
  page.on('response', onResponse);
  return {
    bodies,
    holdNext() {
      const h = { released: false, request: null, sendIndex: -1, answeredAt: undefined };
      h.promise = new Promise((resolve) => { h.release = () => { h.released = true; resolve(); }; });
      h.answered = new Promise((resolve) => { h.resolveAnswered = resolve; });
      holds.push(h);
      hold = h;
      return { release: h.release, sent: page.waitForRequest(isScorePut), answered: h.answered, hold: h };
    },
    async answeredBeforeNextWrite(held) {
      const h = held.hold;
      await Promise.race([
        h.answered,
        new Promise((_, reject) => setTimeout(() => reject(new Error('held score write: its response never reached the page')), safetyMs)),
      ]);
      const next = sends[h.sendIndex + 1];
      if (next && next.at <= h.answeredAt) {
        throw new Error(`held score write: the next write was sent ${h.answeredAt - next.at} ms before the held one's response reached the page, so the race was not exercised`);
      }
    },
    async stop() {
      // Release first: unrouting does not wait for a handler (Playwright
      // 1.63.0), so a PUT still blocked in it would stay held until the safety
      // timer instead of going through now.
      for (const h of holds) h.release();
      page.off('response', onResponse);
      await page.unroute(SCORE_ROUTE, handler);
    },
    assertNoSafetyRelease() {
      if (safetyFired) throw new Error(`held score write: the ${safetyMs} ms safety timeout released ${safetyFired} hold(s) nobody released`);
    },
  };
}
