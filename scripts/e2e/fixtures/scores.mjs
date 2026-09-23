// A competition's Scores tab (/admin/competition/:id/scores,
// admin_schedule_score_editor.jsx): every match as a row with a Score (or
// Correct) button that opens the OVERLAY score editor, the second mount site
// beside the shiaijo page's inline one.
import { expect } from '@playwright/test';
import { EDITOR } from '../../screenshots/lib/editor.mjs';

export const scoreRows = (page) => page.locator('.score-edit-row');

// The first SCHEDULED match on `court`, opened in the overlay editor.
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
