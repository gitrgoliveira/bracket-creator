// The court operator's page, /admin/shiaijo/:court (admin_shiaijo.jsx): the
// queue on the left (Up next, Upcoming, Completed) and the inline score
// editor on the right. Every selector for the court console's own surfaces
// lives here; the editor's insides belong to the scoring fixtures.
import { expect } from '@playwright/test';
import { INLINE_EDITOR } from '../../screenshots/lib/editor.mjs';

export async function openShiaijo(page, court) {
  await page.goto(`/admin/shiaijo/${encodeURIComponent(court)}`);
  await expect(page.getByRole('heading', { level: 1, name: `Shiaijo ${court}` })).toBeVisible();
}

// The queue's parts, as selectors for a caller that combines them (or reads
// them inside the page) and as locators. WAITING_ROWS is Upcoming: the rows
// still to be played, not Later and not Completed.
export const UP_NEXT = '.shiaijo-upnext__card';
export const WAITING_ROWS = '.shiaijo__queue .shiaijo-group:not(.shiaijo-pending) .shiaijo-qrow:not(.shiaijo-qrow--complete)';
export const upNextCard = (page) => page.locator(UP_NEXT);
export const waitingRows = (page) => page.locator(WAITING_ROWS);
export const completedRows = (page) => page.locator('.shiaijo-completed .shiaijo-qrow');
export const inlineEditor = (page) => page.locator(INLINE_EDITOR);
// "Ready when you are": nothing selected while matches remain. And the done
// state, "<competition> is complete on Shiaijo X", once none remain.
export const courtIdle = (page) => page.locator('.shiaijo__placeholder');
export const courtDone = (page) => page.locator('.shiaijo__main > .empty:not(.shiaijo__placeholder)');

// The two names a match row shows, Shiro first. `.numbered-name__text` is the
// name alone, without the competitor number chip beside it.
export async function sides(row) {
  const names = row.locator('.numbered-name__text');
  return { shiro: (await names.nth(0).textContent()).trim(), aka: (await names.nth(1).textContent()).trim() };
}

// Which match an editor holds, as the operator reads it: its eyebrow ("MEN
// INDIVIDUAL · POOL A · MATCH 1 OF 3"), its title line ("Shiaijo B · 10:20",
// which says the court) and the first two names it shows. `key` joins them,
// so two readings tell whether the editor moved to another match. The inline
// editor by default; pass EDITOR for the overlay the Scores tab opens, or any
// editor Locator.
export async function editorIdentity(page, root = INLINE_EDITOR) {
  const ed = typeof root === 'string' ? page.locator(root).first() : root.first();
  const first = async (sel) => ((await ed.locator(sel).allInnerTexts())[0] || '').trim();
  const eyebrow = await first('.editor-modal__eyebrow');
  // The title's first line: the sync pill beside it is not the match.
  const title = (await first('.editor-modal__title')).split('\n')[0].trim();
  const names = (await ed.locator('.numbered-name__text').allInnerTexts()).slice(0, 2).map((t) => t.trim());
  return {
    eyebrow, title, names,
    court: (title.match(/Shiaijo\s+(\S+)/) || [])[1] || null,
    key: [eyebrow, title, ...names].join(' | '),
  };
}

// Start the Up next match the way the operator does, from its card, and
// return the pairing once the inline editor holds it. Only the card's own
// "Start match" is used: every Upcoming row carries one too. The Start goes
// through the outbox and never waits (bc-evts): the editor holds the match at
// once, and its board shows every tap from then on, answered or not.
export async function startUpNext(page) {
  const card = upNextCard(page);
  const pair = await sides(card);
  await card.getByRole('button', { name: 'Start match' }).tap();
  const editor = inlineEditor(page);
  await expect(editor.locator('.sb-side--shiro')).toContainText(pair.shiro);
  await expect(editor.locator('.sb-side--aka')).toContainText(pair.aka);
  return pair;
}

// The newest Completed row whose names match a pairing (the Completed list is
// oldest first, so the newest is its tail).
export function completedRowFor(page, { shiro, aka }) {
  return completedRows(page).filter({ hasText: shiro }).filter({ hasText: aka }).last();
}

// The newest completed match with its score string, e.g. "M vs –".
export async function lastCompleted(page) {
  const row = completedRows(page).last();
  await expect(row).toBeVisible();
  return { ...(await sides(row)), result: (await row.locator('.shiaijo-qrow__result').innerText()).trim() };
}
