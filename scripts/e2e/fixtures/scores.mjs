// A competition's Scores tab (/admin/competition/:id/scores,
// admin_schedule_score_editor.jsx): every match as a row with a Score (or
// Correct) button that opens the OVERLAY score editor, the second mount site
// beside the shiaijo page's inline one.
import { expect } from '@playwright/test';
import { EDITOR } from '../../screenshots/lib/editor.mjs';

export const scoreRows = (page) => page.locator('.score-edit-row');

// The first match on `court` whose row offers Score (a scheduled or running
// one; a finished match's row offers Correct), opened in the overlay editor.
// Returns the row it opened, e.g. for reading the pairing.
export async function openScoreEditor(page, id, { court } = {}) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/scores`);
  let rows = scoreRows(page).filter({ has: page.getByRole('button', { name: /^Score$/ }) });
  if (court) rows = rows.filter({ has: page.locator('.score-edit-row__court', { hasText: new RegExp(`^${court}\\b`) }) });
  const row = rows.first();
  await row.getByRole('button', { name: /^Score$/ }).tap();
  await expect(page.locator(EDITOR)).toBeVisible();
  return row;
}

// The RUNNING match on `court` (the row the server marks running), opened in
// the overlay editor: how a second device scores a match the court console
// runs. A court console that does not run its shiaijo is view only (FR9), so a
// second tablet at the same court cannot score; the Scores page can, as the
// head table does. Waits for the row, since the match may have been started a
// moment ago on another device. Returns the row it opened.
export async function openRunningScoreEditor(page, id, { court } = {}) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/scores`);
  let rows = scoreRows(page).and(page.locator('.score-edit-row--running'));
  if (court) rows = rows.filter({ has: page.locator('.score-edit-row__court', { hasText: new RegExp(`^${court}\\b`) }) });
  const row = rows.first();
  await expect(row, 'the running match is listed on the Scores page').toBeVisible();
  await row.getByRole('button', { name: /^Score$/ }).tap();
  await expect(page.locator(EDITOR)).toBeVisible();
  return row;
}
