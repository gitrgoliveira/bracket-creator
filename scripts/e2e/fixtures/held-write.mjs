// Hold a running match's score write in flight, so a race with its echo can be
// observed. On localhost the answer to an autosave arrives within milliseconds
// of the save going out, so a tap made "while the save is out" never lands in
// that window by itself; the write is held until the test releases it, and it
// does so only after the next tap has been dispatched, as a slow uplink would
// hold it. A safety timeout lets go of a hold nobody released.
const SCORE_ROUTE = '**/api/competitions/*/matches/*/score';
const isScorePut = (r) => r.method() === 'PUT' && /\/score$/.test(r.url());

// Start holding on `page`. Returns:
//   holdNext()  the next score PUT is held; returns { release, sent } where
//               `sent` resolves once that PUT has been sent (so the autosave
//               debounce has run) and `release()` lets it through
//   bodies      the JSON body of every score PUT the page sent, in order
//   stop()      remove the route (call it in a finally)
export async function holdScoreWrites(page, { safetyMs = 10_000 } = {}) {
  let hold = null;
  const bodies = [];
  const handler = async (route) => {
    const req = route.request();
    if (isScorePut(req)) {
      try { bodies.push(req.postDataJSON()); } catch { bodies.push(null); }
      if (hold) {
        const { promise } = hold;
        hold = null;
        await Promise.race([promise, new Promise((r) => setTimeout(r, safetyMs))]);
      }
    }
    await route.continue();
  };
  await page.route(SCORE_ROUTE, handler);
  return {
    bodies,
    holdNext() {
      const h = {};
      h.promise = new Promise((resolve) => { h.release = resolve; });
      hold = h;
      return { release: h.release, sent: page.waitForRequest(isScorePut) };
    },
    stop: () => page.unroute(SCORE_ROUTE, handler),
  };
}
