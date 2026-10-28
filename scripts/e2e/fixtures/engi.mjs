// Scoring an ENGI (kata pairs) match on the flag editor
// (admin_scoring_engi.jsx). An engi competitor is a pair, rostered as one
// participant ("Name 1 - Name 2, Dojo"), and a match is scored in flags, not
// ippon: each side has a counter, and the total must be 1, 3 or 5. The editor
// is the same whether the shiaijo page mounts it inline or the Scores tab
// opens it as an overlay; the helpers default to the inline one.
import { expect } from '@playwright/test';
import { INLINE_EDITOR } from '../../screenshots/lib/editor.mjs';
import { beforeFinish, finishLanded, settled } from './scoring.mjs';
import { armTwoTap } from './pace.mjs';
import { sides, upNextCard } from './shiaijo.mjs';

// The editor panel itself (inline) or its dialog (overlay).
export const engiEditor = (page, root = INLINE_EDITOR) => page.locator(root).filter({ has: page.getByTestId('engi-submit') }).first();

const SIDE_KEY = { aka: 'aka', shiro: 'shiro' };

// A side's flag count as the board shows it, and its + / - buttons.
export const flagCount = (page, side, root = INLINE_EDITOR) => engiEditor(page, root).getByTestId(`engi-${SIDE_KEY[side]}-count`);
export const flagButton = (page, side, change, root = INLINE_EDITOR) => engiEditor(page, root)
  .getByTestId(`engi-${SIDE_KEY[side]}-${change === '+' ? 'inc' : 'dec'}`);
export const engiTotal = (page, root = INLINE_EDITOR) => engiEditor(page, root).getByTestId('engi-total');
export const engiSubmit = (page, root = INLINE_EDITOR) => engiEditor(page, root).getByTestId('engi-submit');

// Wait until every flag tapped on this device has been answered by the server
// (the outbox is empty).
export const engiSettle = (page) => settled(page);

// The pair each side of the open editor holds, by the first member's line.
export async function engiPair(page, root = INLINE_EDITOR) {
  const ed = engiEditor(page, root);
  const first = async (side) => (await ed.getByTestId(`engi-side-${side}`).locator('.numbered-name__text').first().textContent()).trim();
  return { shiro: await first('shiro'), aka: await first('aka') };
}

// Start the Up next match from its card and return its pair once the engi
// editor holds it.
export async function startUpNextEngi(page) {
  const card = upNextCard(page);
  const pair = await sides(card);
  await card.getByRole('button', { name: 'Start match' }).tap();
  await expect(engiEditor(page)).toBeVisible();
  const held = await engiPair(page);
  // Each card holds its own pair: Shiro's the one the Up next card showed on
  // Shiro, Aka's the one it showed on Aka.
  const firstMember = (name) => name.split(' - ')[0].trim();
  expect(held.shiro, 'the Shiro card holds the Shiro pair').toBe(firstMember(pair.shiro));
  expect(held.aka, 'the Aka card holds the Aka pair').toBe(firstMember(pair.aka));
  return held;
}

// Enter flags: tap + the given number of times on each side. Each tap is
// checked against the count it should show next, so a dropped tap fails at
// once. No dwell between taps: engi counters are deliberately not tap-guarded.
export async function enterFlags(page, { aka = 0, shiro = 0 }, root = INLINE_EDITOR) {
  const expected = {};
  for (const [side, taps] of [['aka', aka], ['shiro', shiro]]) {
    const count = flagCount(page, side, root);
    const start = Number(await count.innerText());
    expected[side] = start + taps;
    for (let i = 0; i < taps; i += 1) {
      await flagButton(page, side, '+', root).tap();
      await expect(count, `the ${side} count shows the tap`).toHaveText(String(start + i + 1));
    }
  }
  await expect(flagCount(page, 'aka', root)).toHaveText(String(expected.aka));
  await expect(flagCount(page, 'shiro', root)).toHaveText(String(expected.shiro));
}

// Save the result in two taps ("Save result" / "Finish + Start Next →", then
// "Tap again to ..."), and return once the write has landed (finishLanded).
export async function saveEngiResult(page, root = INLINE_EDITOR) {
  const ed = page.locator(root).first();
  const armed = await armTwoTap(engiSubmit(page, root), engiSubmit(page, root).filter({ hasText: /^Tap again to/ }));
  const before = await beforeFinish(ed);
  await armed.tap();
  return finishLanded(ed, before);
}
