// Scoring an INDIVIDUAL bout on the kendo board (admin_scoring_individual.jsx).
// The board is the same whether it is mounted inline on the shiaijo page
// (INLINE_EDITOR) or as the overlay dialog the Scores tab opens (EDITOR), so
// every helper takes the editor's selector. The team and kachinuki editors
// score differently and get their own helpers when a journey needs them.
//
// Start and Finish come from scripts/screenshots/lib/editor.mjs (startMatch,
// finishMatch), re-exported here so a journey imports one scoring module.
import { expect } from '@playwright/test';
import { EDITOR, INLINE_EDITOR, finishMatch, startMatch } from '../../screenshots/lib/editor.mjs';

export { EDITOR, INLINE_EDITOR, finishMatch, startMatch };

const SIDE_NAME = { shiro: 'Shiro', aka: 'Aka' };

// An ippon button: one waza letter (M, K, D, T, H, and S on naginata) on a
// side of the board. Matched whole, since a substring test would be ambiguous
// the day a two-letter waza appears.
export const ipponButton = (page, side, waza, root = INLINE_EDITOR) => page.locator(root)
  .locator(`.sb-side--${side} .ipt-btn`, { hasText: new RegExp(`^${waza}$`) }).first();

// The autosave indicator on the open editor ("Synced", "Syncing…",
// "Offline", ...; admin_scoring_autosave.jsx).
export const syncState = (page, root = INLINE_EDITOR) => page.locator(root).locator('.sync-pill__label').first();

// The slots a side's points fill, outside to inside. A filled slot is labelled
// "<Side> slot <n>: remove <waza>" (tapping it clears the mark).
export const filledSlots = (page, side, root = INLINE_EDITOR) => page.locator(root)
  .locator(`[aria-label^="${SIDE_NAME[side]} slot "]:not([aria-label$=": empty"])`);

// Award one ippon and return once the board shows it and the autosave has
// landed. `side` is 'shiro' | 'aka'.
export async function awardIppon(page, side, waza = 'M', root = INLINE_EDITOR) {
  const before = await filledSlots(page, side, root).count();
  await ipponButton(page, side, waza, root).tap();
  await expect(filledSlots(page, side, root)).toHaveCount(before + 1);
  await expect(syncState(page, root)).toHaveText('Synced');
}

// The Finish button in its first state ("Finish" or "Finish + Start Next →"),
// and the armed state the first tap turns it into.
export const finishButton = (page, root = INLINE_EDITOR) => page.locator(root)
  .locator('button').filter({ hasText: /^Finish( \+ Start Next|$)/ }).first();
export const armedFinishButton = (page, root = INLINE_EDITOR) => page.locator(root)
  .locator('button').filter({ hasText: /^Tap again to finish/ }).first();

// Tap Finish once: the two-tap guard arms and nothing is submitted yet.
export async function armFinish(page, root = INLINE_EDITOR) {
  await finishButton(page, root).tap();
  await expect(armedFinishButton(page, root)).toBeVisible();
  return armedFinishButton(page, root);
}
