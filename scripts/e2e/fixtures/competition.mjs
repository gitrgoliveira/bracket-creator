// A competition's own admin pages: the roster paste box (Participants &
// seeds), Generate draw, Discard draw and Start competition.
import { expect } from '@playwright/test';

const compPath = (id, section = '') => `/admin/competition/${id}${section ? `/${section}` : ''}`;

// Paste a roster into the Participants page's LinedTextarea and apply it.
// `rows` is a list of [name, dojo] pairs or of ready-made lines. Every row
// needs a dojo, and (name, dojo) must be unique: the roster save refuses
// either, and that refusal is not what this helper is for.
export async function pasteRoster(page, id, rows) {
  const lines = rows.map((r) => (Array.isArray(r) ? r.join(', ') : r));
  if (!page.url().endsWith(compPath(id, 'participants'))) await page.goto(compPath(id, 'participants'));
  const box = page.locator('.lined-textarea__area');
  await box.fill(lines.join('\n'));
  // The page renders TWO identical "Apply changes" buttons, one above the
  // box and one under the preview table; either applies.
  await page.getByRole('button', { name: 'Apply changes' }).first().tap();
  // Applying the first roster moves the page on to the competition overview;
  // the header's count is what says the roster landed.
  await expect(page.getByText(new RegExp(`\\b${lines.length} (players|teams)\\b`)).first()).toBeVisible();
  await expect(page.getByText('Not applied yet')).toHaveCount(0);
}

// Generate the draw from the competition header, and return once the
// competition reads "Draw ready" (the header then offers Discard draw).
export async function generateDraw(page, id) {
  const btn = page.getByRole('button', { name: 'Generate draw' });
  if (!(await btn.isVisible())) await page.goto(compPath(id));
  await btn.tap();
  await expect(page.getByRole('button', { name: 'Discard draw' })).toBeVisible();
}

// Tap Discard draw. It asks through the app's confirm dialog, which is left
// open for the caller: answering it is the journey's decision (or the clumsy
// operator's, see hastyConfirm in clumsy.mjs).
export async function requestDiscardDraw(page) {
  await page.getByRole('button', { name: 'Discard draw' }).tap();
  await expect(page.locator('.modal[role="dialog"] .modal__foot')).toBeVisible();
}

// Start the competition. There is no confirm: the tap starts it and lands on
// the Scores tab.
export async function startCompetition(page, id) {
  const btn = page.getByRole('button', { name: /^Start competition/ });
  if (!(await btn.isVisible())) await page.goto(compPath(id));
  await btn.tap();
  await expect(page).toHaveURL(new RegExp(`${compPath(id, 'scores')}$`));
}
