// A competition's own admin pages: the roster paste box (Participants &
// seeds), Generate draw, Publish draw and Unpublish, Discard draw and Start
// competition.
import { expect } from '@playwright/test';
import { dwell } from './pace.mjs';

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

// Discard the draw and answer its confirm, after the confirm's own bounce
// window, with the button that discards. Returns once the header offers
// Generate draw again.
export async function discardDraw(page) {
  await requestDiscardDraw(page);
  const dlg = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();
  await dwell(page, Date.now());
  await dlg.getByRole('button', { name: 'Discard draw' }).tap();
  await expect(page.getByRole('button', { name: 'Generate draw' })).toBeVisible();
}

// The words of the draw's visibility on the draw preview header
// (web-mobile/js/draw_visibility.jsx): the toggle's two labels, the hint line
// under the buttons in each state, and the toasts a tap leaves.
export const DRAW_VISIBILITY = {
  publish: 'Publish draw',
  unpublish: 'Unpublish',
  hiddenHint: 'Hidden from the public. Publish draw or Start competition makes it public.',
  publishedHint: 'Published. The public can see this draw. Changes you make show on public pages at once.',
  publishedToast: 'Draw published. The public can see it now.',
  unpublishedToast: 'Hidden again. Anyone who looked still has what they saw.',
  // The Start confirm on a hidden draw, for a competition called `name`.
  startConfirm: (name) => `The draw for "${name}" is still hidden from the public. Starting the competition makes it public at once.`,
};

// The draw preview header's visibility toggle and hint line.
export const drawVisibilityToggle = (page) => page.getByTestId('draw-visibility-toggle');
export const drawVisibilityHint = (page) => page.getByTestId('draw-visibility-hint');

// Publish a draft's draw from the draw preview header, so the public pages
// show its pools and bracket. A generated draw starts hidden: any journey that
// reads a draw-ready competition's public pages publishes it first. Returns
// once the header reads published (the toggle offers Unpublish).
export async function publishDraw(page, id) {
  const toggle = drawVisibilityToggle(page);
  if (!(await toggle.isVisible())) await page.goto(compPath(id));
  await expect(toggle).toHaveText(DRAW_VISIBILITY.publish);
  await toggle.tap();
  await expect(toggle).toHaveText(DRAW_VISIBILITY.unpublish);
  await expect(drawVisibilityHint(page)).toHaveText(DRAW_VISIBILITY.publishedHint);
}

// Hide a published draft's draw again (Unpublish). Returns once the header
// reads hidden (the toggle offers Publish draw).
export async function unpublishDraw(page, id) {
  const toggle = drawVisibilityToggle(page);
  if (!(await toggle.isVisible())) await page.goto(compPath(id));
  await expect(toggle).toHaveText(DRAW_VISIBILITY.unpublish);
  await toggle.tap();
  await expect(toggle).toHaveText(DRAW_VISIBILITY.publish);
  await expect(drawVisibilityHint(page)).toHaveText(DRAW_VISIBILITY.hiddenHint);
}

// Start the competition and land on the Scores tab. A hidden draw asks first
// (the draw becomes public at once), and the helper answers that confirm with
// Start competition, after the confirm's own bounce window; a published draw
// starts in the same tap. Returns the confirm's message, or null when none
// was asked, so a journey can check the question as well as the start.
export async function startCompetition(page, id) {
  const btn = page.getByRole('button', { name: /^Start competition/ });
  if (!(await btn.isVisible())) await page.goto(compPath(id));
  await btn.tap();
  const scores = new RegExp(`${compPath(id, 'scores')}$`);
  const dlg = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();
  await expect.poll(async () => scores.test(page.url()) || (await dlg.isVisible()), {
    message: 'Start competition either starts the competition or asks about its hidden draw',
  }).toBe(true);
  let asked = null;
  if (!scores.test(page.url())) {
    asked = (await dlg.locator('.modal__body').innerText()).trim();
    await dwell(page, Date.now());
    await dlg.getByRole('button', { name: 'Start competition' }).tap();
  }
  await expect(page).toHaveURL(scores);
  return asked;
}
