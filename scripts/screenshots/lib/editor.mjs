// What every recipe that drives a score editor agrees on: which element the
// editor IS, how a match is started, and how its Finish commits. Everything else about driving one
// (how a bout is scored, how a fighter is named) differs between the
// individual, fixed-order team and kachinuki editors, so it stays in the recipe
// that needs it; see the note at the end of lib/ui.mjs.

// The editor dialog. Every overlay editor renders `.modal-backdrop
// [data-testid="scoring-modal-root"] > .editor-modal`
// (admin_scoring_individual.jsx, admin_scoring_team.jsx, admin_scoring_engi.jsx),
// so the testid names the BACKDROP and the class names the dialog itself.
export const EDITOR = '.editor-modal';

// The editors ignore a repeat POINTER tap within their bounce window
// (TAP_BOUNCE_MS, 400ms, in web-mobile/js/tap_guard.jsx): a second ippon or
// foul on the same side of a bout, and the confirming tap of a two-tap Finish
// or End match that came too soon after the arming one. A Playwright click is
// a pointer tap, so a recipe waits at least this long between two such taps
// or the second one is dropped.
export const TAP_DWELL_MS = 500;

// sameSideTapPacer: for a recipe that taps ippons from a list, returns an
// async `pace(page, side)` to await just before each tap. It waits out
// TAP_DWELL_MS since that side's previous tap and lets any other tap through
// at once, so a list stays as fast as its own gaps allow.
export function sameSideTapPacer() {
  const last = {};
  return async (page, side) => {
    const since = Date.now() - (last[side] ?? -Infinity);
    if (since < TAP_DWELL_MS) await page.waitForTimeout(TAP_DWELL_MS - since);
    last[side] = Date.now();
  };
}

// Finish is a two-tap guard on the individual and fixed-order team editors:
// the first tap arms the button ("Tap again to finish"), only the second
// submits (admin_scoring_individual.jsx and admin_scoring_team.jsx, the
// `finishArmed` label). Its label is "Finish + Start Next →" instead whenever
// another match waits on the same shiaijo, and that form leaves the editor
// open on the next match; whether to dismiss it is the caller's business.
//
// The arm is waited for, never probed: a probe on the same tick misses it, the
// second tap is skipped, and the next taps score a match the caller did not
// mean. If it never appears the wait throws, because a Finish that did not arm
// did not finish. The second tap then waits out the bounce window, which
// would otherwise swallow it (TAP_DWELL_MS). Not for the engi editor ("Save
// result" commits in one tap) or a correction ("Save correction" does not arm
// either).
export async function finishMatch(page) {
  const modal = page.locator(EDITOR).first();
  await modal.locator('button').filter({ hasText: /^Finish( \+ Start Next|$)/ }).first().click();
  const armed = modal.locator('button').filter({ hasText: /^Tap again to finish/ }).first();
  await armed.waitFor({ state: 'visible', timeout: 3000 });
  await page.waitForTimeout(TAP_DWELL_MS);
  await armed.click();
  // Return once the write has landed, not after a guessed pause. The button
  // reads "Saving…" from the second tap until the server answers, then either
  // the editor closes (plain Finish) or it moves to the next match and the
  // button takes that match's own label (Finish + Start Next). Either way no
  // button reads the arm or "Saving…" any more, and the arm label covers the
  // tick before "Saving…" first renders.
  await modal.locator('button').filter({ hasText: /^(Tap again to finish|Saving…)/ }).first()
    .waitFor({ state: 'hidden', timeout: 15000 });
}

// Start the open match if the editor offers it, and return once the start has
// landed: "Start match" is offered only while the match is scheduled, so the
// button leaving is the server having it running.
export async function startMatch(page) {
  const btn = page.locator(EDITOR).locator('button').filter({ hasText: /^Start match$/ }).first();
  if (!(await btn.count())) return;
  await btn.click();
  await btn.waitFor({ state: 'hidden', timeout: 15000 });
}
