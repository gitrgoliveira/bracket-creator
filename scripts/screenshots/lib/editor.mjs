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

// The same editor mounted INLINE, as the shiaijo page (/admin/shiaijo/:court)
// does: admin_scoring_individual.jsx renders `variant="inline"` as
// `<div class="scoring-panel editor-modal--compact">`, with no backdrop and no
// `.editor-modal` class, so EDITOR matches nothing there. finishMatch takes
// the editor's selector as `root` for that reason; the default keeps every
// overlay caller unchanged. startMatch does not: on that page "Start match"
// lives on the Up next card, outside the editor, and the card's button never
// leaves (it moves on to the next match), so startMatch's wait would not hold.
export const INLINE_EDITOR = '.scoring-panel';

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
export async function finishMatch(page, root = EDITOR) {
  const modal = page.locator(root).first();
  await modal.locator('button').filter({ hasText: /^Finish( \+ Start Next|$)/ }).first().click();
  const armed = modal.locator('button').filter({ hasText: /^Tap again to finish/ }).first();
  await armed.waitFor({ state: 'visible', timeout: 3000 });
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
