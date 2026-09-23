// What every recipe that drives a score editor agrees on: which element the
// editor IS, and how its Finish commits. Everything else about driving one
// (how a bout is scored, how a fighter is named) differs between the
// individual, fixed-order team and kachinuki editors, so it stays in the recipe
// that needs it; see the note at the end of lib/ui.mjs.

// The editor dialog. Every overlay editor renders `.modal-backdrop
// [data-testid="scoring-modal-root"] > .editor-modal`
// (admin_scoring_individual.jsx, admin_scoring_team.jsx, admin_scoring_engi.jsx),
// so the testid names the BACKDROP and the class names the dialog itself.
export const EDITOR = '.editor-modal';

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
// did not finish. Not for the engi editor ("Save result" commits in one tap)
// or a correction ("Save correction" does not arm either).
export async function finishMatch(page) {
  const modal = page.locator(EDITOR).first();
  await modal.locator('button').filter({ hasText: /^Finish( \+ Start Next|$)/ }).first().click();
  const armed = modal.locator('button').filter({ hasText: /^Tap again to finish/ }).first();
  await armed.waitFor({ state: 'visible', timeout: 3000 });
  await armed.click();
}
