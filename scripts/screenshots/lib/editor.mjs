// What every recipe that drives a score editor agrees on: which element the
// editor IS, how a match is started, and how its Finish commits. Everything else about driving one
// (how a bout is scored, how a fighter is named) differs between the
// individual, team and kachinuki editors, so it stays in the recipe
// that needs it; see the note at the end of lib/ui.mjs.

// The editor dialog. Every overlay editor renders `.modal-backdrop
// [data-testid="scoring-modal-root"] > .editor-modal`
// (admin_scoring_individual.jsx, admin_scoring_team.jsx, admin_scoring_engi.jsx),
// so the testid names the BACKDROP and the class names the dialog itself.
export const EDITOR = '.editor-modal';

// The same editor mounted INLINE, as the shiaijo page (/admin/shiaijo/:court)
// does: every editor's `variant="inline"` renders a `.scoring-panel` with no
// backdrop and no `.editor-modal` class (admin_scoring_individual.jsx and
// admin_scoring_engi.jsx as `scoring-panel editor-modal--compact`,
// admin_scoring_team.jsx as `scoring-panel scoring-panel--team`, and the
// lineup panel's inline variant in admin_schedule_lineup.jsx as `scoring-panel
// lineup-panel--inline`), so EDITOR matches nothing there and this matches all
// of them. The e2e journeys pass it as the editor's root
// (scripts/e2e/fixtures/scoring.mjs); finishMatch and startMatch below drive
// the overlay only.
export const INLINE_EDITOR = '.scoring-panel';

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

// Finish is a two-tap guard on every editor: the first tap arms the button
// ("Tap again to finish"), only the second submits (admin_scoring_individual.jsx
// and admin_scoring_team.jsx, the `finishArmed` label). The engi editor's is
// "Save result", armed as "Tap again to save" (admin_scoring_engi.jsx,
// `saveArmed`). The overlay editors these recipes drive are the admin hosts',
// whose Finish never starts the next match (the court console's inline editor
// alone offers "Finish + Start Next"), so after it the editor closes; to go on,
// see startNextFromList below.
//
// The arm is waited for, never probed: a probe on the same tick misses it, the
// second tap is skipped, and the next taps score a match the caller did not
// mean. If it never appears the wait throws, because a Finish that did not arm
// did not finish. The second tap then waits out the bounce window, which
// would otherwise swallow it (TAP_DWELL_MS). Not for a correction ("Save
// correction" does not arm).
export async function finishMatch(page) {
  const modal = page.locator(EDITOR).first();
  await modal.locator('button').filter({ hasText: /^(Finish|Save result)$/ }).first().click();
  // The bounce window runs from the arming tap, so the wait for the arm label
  // counts towards it.
  const armedAt = Date.now();
  const armed = modal.locator('button').filter({ hasText: /^Tap again to (finish|save)/ }).first();
  await armed.waitFor({ state: 'visible', timeout: 3000 });
  await page.waitForTimeout(Math.max(0, TAP_DWELL_MS - (Date.now() - armedAt)));
  const landed = scoreWriteAnswered(page, 'completed');
  await armed.click();
  // Return once the write has landed, not after a guessed pause. The editor
  // hands the finish to the outbox and moves on at once (it never waits on
  // the network), so the editor's own state says nothing about the server:
  // wait for the server's answer to the finish itself.
  await landed;
}

// Start the open match if the editor offers it, and return once the start has
// landed. The editor shows its own start at once, before the server has it,
// so the button leaving is not the server's answer: the start's own answer is.
export async function startMatch(page) {
  const btn = page.locator(EDITOR).locator('button').filter({ hasText: /^Start match$/ }).first();
  if (!(await btn.count())) return;
  const landed = scoreWriteAnswered(page, 'running');
  await btn.click();
  await landed;
}

// On the Scores tab (/admin/score-editor, /admin/competition/:id/scores) Finish
// records the result and closes the editor: it never starts the next match,
// which is the court console's alone. A recipe playing matches in a row opens
// the next one from the list and starts it here. The finished match is waited
// out of the list's running rows first, so the row opened is never the one
// just finished, shown running until the list's refresh.
export async function startNextFromList(page) {
  await page.locator(EDITOR).first().waitFor({ state: 'hidden', timeout: 15000 });
  await page.waitForFunction(() => !document.querySelector('.score-edit-row.is-running'), null, { timeout: 15000 });
  const row = page.locator('.score-edit-row:not(.score-edit-row--complete)')
    .filter({ has: page.locator('button', { hasText: /^Score$/ }) }).first();
  await row.waitFor({ state: 'visible', timeout: 15000 });
  await row.locator('button').filter({ hasText: /^Score$/ }).first().click();
  await page.locator(EDITOR).first().waitFor({ state: 'visible', timeout: 15000 });
  await startMatch(page);
}

// scoreWriteAnswered: resolves once the server has answered a score write of
// a match (PUT .../matches/:id/score) whose status is `status`, as the outbox
// sends it. Armed BEFORE the tap that makes the write, so a quick answer is
// never missed; a refusal (an error status) throws, since a recipe that went
// on would capture a match the server never took.
export async function scoreWriteAnswered(page, status, timeout = 15000) {
  const res = await page.waitForResponse((r) => {
    const req = r.request();
    if (req.method() !== 'PUT' || !/\/api\/competitions\/[^/]+\/matches\/[^/]+\/score$/.test(new URL(r.url()).pathname)) return false;
    try { return (req.postDataJSON() || {}).status === status; } catch { return false; }
  }, { timeout });
  if (!res.ok()) throw new Error(`the ${status} score write was refused: HTTP ${res.status()}`);
  return res;
}
