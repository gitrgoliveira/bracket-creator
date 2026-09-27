// The court operator's page, /admin/shiaijo/:court (admin_shiaijo.jsx): the
// queue on the left (Up next, Upcoming, Completed) and the inline score
// editor on the right.
import { expect } from '@playwright/test';
import { INLINE_EDITOR } from '../../screenshots/lib/editor.mjs';

export async function openShiaijo(page, court) {
  await page.goto(`/admin/shiaijo/${encodeURIComponent(court)}`);
  await expect(page.getByRole('heading', { level: 1, name: `Shiaijo ${court}` })).toBeVisible();
}

export const upNextCard = (page) => page.locator('.shiaijo-upnext__card');
export const completedRows = (page) => page.locator('.shiaijo-completed .shiaijo-qrow');
export const inlineEditor = (page) => page.locator(INLINE_EDITOR);

// The two names a match row shows, Shiro first. `.numbered-name__text` is the
// name alone, without the competitor number chip beside it.
export async function sides(row) {
  const names = row.locator('.numbered-name__text');
  return { shiro: (await names.nth(0).textContent()).trim(), aka: (await names.nth(1).textContent()).trim() };
}

// The "MEN INDIVIDUAL · POOL A · MATCH 1 OF 3" eyebrow over the running
// match: what tells the operator which match the editor holds.
export async function editorIdentity(page) {
  return (await inlineEditor(page).locator('.editor-modal__eyebrow').first().innerText()).trim();
}

// Start the Up next match the way the operator does, from its card, and
// return the pairing once the inline editor holds it. Only the card's own
// "Start match" is used: every Upcoming row carries one too.
export async function startUpNext(page) {
  const card = upNextCard(page);
  const pair = await sides(card);
  await card.getByRole('button', { name: 'Start match' }).tap();
  const editor = inlineEditor(page);
  await expect(editor.locator('.sb-side--shiro')).toContainText(pair.shiro);
  await expect(editor.locator('.sb-side--aka')).toContainText(pair.aka);
  return pair;
}

// The newest completed match (the Completed list is oldest first, so the
// newest is its tail) with its score string, e.g. "M vs –".
export async function lastCompleted(page) {
  const row = completedRows(page).last();
  await expect(row).toBeVisible();
  return { ...(await sides(row)), result: (await row.locator('.shiaijo-qrow__result').innerText()).trim() };
}
