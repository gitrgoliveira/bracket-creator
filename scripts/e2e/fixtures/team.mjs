// Fixed-order TEAM competitions: naming team members, the three lineup entry
// points, and scoring a bout inside a team encounter.
//
//   the Lineups page      /admin/competition/:id/lineups (admin_lineup.jsx)
//                         writes the ROUND lineup, one team at a time
//   the match panel       "Enter lineup" / "Lineup" on the shiaijo queue
//                         (admin_schedule_lineup.jsx), writes the MATCH lineup
//   the score sheet row   the name box on each bout row of the team editor
//                         (admin_scoring_team.jsx), writes the MATCH lineup
//
// The shared scoring helpers (fixtures/scoring.mjs) scope to the INDIVIDUAL
// board's `.sb-side--*` wrappers, which the team sheet does not have, so the
// team sheet's own selectors live here. Every helper performs a real tap on a
// real control; none of them asserts app behaviour beyond "the tap landed".
import { expect } from '@playwright/test';
import { createTournament, login } from './setup.mjs';

// FIK position names for a five-person team, in order; any other size uses
// bare numbers (admin_lineup.jsx positionsForSize).
export const FIK5 = ['Senpo', 'Jiho', 'Chuken', 'Fukusho', 'Taisho'];
export const positionLabels = (teamSize) => (teamSize === 5 ? FIK5 : Array.from({ length: teamSize }, (_, i) => String(i + 1)));

// The team sheet's per-row position name, used in its aria-labels
// (admin_scoring_team.jsx positionLabelFor): FIK names at five, else "Match N".
export const sheetPositionLabel = (teamSize, idx) => (teamSize === 5 ? FIK5[idx] : `Match ${idx + 1}`);

// Sign in, creating the tournament first if this server has none yet. Every
// test in a serial file can call it, whichever runs first.
export async function enterAdmin(page, { courts = 16 } = {}) {
  await page.goto('/');
  const welcome = page.getByRole('heading', { name: 'Welcome to Bracket Creator' });
  const first = await welcome.waitFor({ state: 'visible', timeout: 5000 }).then(() => true, () => false);
  if (first) {
    await createTournament(page, { name: 'Team Cup', courts });
    return;
  }
  await login(page);
}

// ---------------------------------------------------------------- Lineups page

const overlineField = (page, text) => page.locator('label')
  .filter({ has: page.locator('.overline', { hasText: new RegExp(`^${text}$`, 'i') }) }).first();

export const lineupsForm = (page) => page.getByTestId('lineup-form-root');

// Open the Lineups page on `team` (its display name) and `round` (1-based,
// as the page shows it).
export async function openLineups(page, id, { team, round = 1 } = {}) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/lineups`);
  await expect(lineupsForm(page)).toBeVisible();
  if (team) {
    const sel = overlineField(page, 'Team').locator('select');
    await sel.selectOption({ label: team });
    await expect(lineupsForm(page).locator('h2')).toContainText(team);
  }
  if (round !== 1) {
    await overlineField(page, 'Round').locator('input').fill(String(round));
  }
  await expect(lineupsForm(page)).toContainText(`Round ${round}`);
  // The team members load separately from the lineup; wait for their list.
  await expect(lineupsForm(page).getByText('Team members', { exact: true })).toBeVisible();
}

// The position's <select> on the Lineups page ("Senpo player", "2 player").
export const lineupsSelect = (page, label) => lineupsForm(page).getByRole('combobox', { name: `${label} player` });

// Pick the member whose option text contains `text` (e.g. "T1.2" or a name).
export async function lineupsPick(page, label, text) {
  const sel = lineupsSelect(page, label);
  const value = await sel.locator('option').filter({ hasText: text }).first().getAttribute('value');
  await sel.selectOption(value);
  await expect(sel).toHaveValue(value);
  return value;
}

// "+ Add new member…" on a position, type `name`, tap Add. Leaves the confirm
// dialog ("Name slot" / "Add member") open for the caller, or returns the
// inline refusal when there is no dialog. Returns { dialog: bool, alert }.
export async function lineupsStartAdd(page, label, name) {
  await lineupsSelect(page, label).selectOption('__add__');
  const input = lineupsForm(page).getByRole('textbox', { name: `New member name for ${label}` });
  await input.fill(name);
  await lineupsForm(page).getByRole('button', { name: /^Add$/ }).tap();
  const dialog = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') });
  const alert = lineupsForm(page).locator('.alert--error');
  await expect(dialog.or(alert).first()).toBeVisible();
  return { dialog: await dialog.isVisible(), alert: (await alert.isVisible()) ? (await alert.innerText()).trim() : '' };
}

// Answer the open confirm dialog with its named button.
export async function answerDialog(page, label) {
  const dialog = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();
  await dialog.getByRole('button', { name: label }).tap();
  await dialog.waitFor({ state: 'hidden' });
}

// Name a position's blank seeded slot through "+ Add new member…" and the
// "Name slot" confirm, the way the docs describe it.
export async function lineupsNameSlot(page, label, name) {
  const r = await lineupsStartAdd(page, label, name);
  if (r.dialog) await answerDialog(page, /^(Name slot|Add member)$/);
  await expect(lineupsSelect(page, label).locator('option:checked')).toContainText(name);
}

export async function lineupsSave(page) {
  await lineupsForm(page).getByRole('button', { name: 'Save lineup' }).tap();
  await expect(page.getByText('Lineup saved').first()).toBeVisible();
}

// The option text currently selected at a position (e.g. "T1.2 Ito").
export const lineupsSelected = async (page, label) => (await lineupsSelect(page, label).locator('option:checked').innerText()).trim();

// ---------------------------------------------------------------- match panel

// One side of the panel: 0 = Shiro (left), 1 = Aka (right).
export const panelSide = (page, i) => page.locator('[data-testid^="match-lineup-side-"]').nth(i);
export const panelInput = (page, i, label) => panelSide(page, i).getByRole('textbox', { name: `${label} player`, exact: true });

// Open the Up next card's "Enter lineup".
export async function openPanelFromUpNext(page) {
  await page.locator('.shiaijo-upnext__card').getByRole('button', { name: 'Enter lineup' }).tap();
  await expect(panelSide(page, 1)).toBeVisible();
}

// Tap a LineupNameInput and choose the option containing `text`. Works on the
// panel and on the score sheet row alike (both render LineupNameInput).
export async function pickFromNameBox(input, text) {
  await input.tap();
  const box = input.locator('xpath=ancestor::div[contains(concat(" ", normalize-space(@class), " "), " lineup-name ")][1]');
  const option = box.locator('.pmf__option').filter({ hasText: text }).first();
  await expect(option).toBeVisible();
  await option.tap();
}

// Close an open name list the way a thumb does: a tap on empty page margin.
// (Escape leaves the box focused, and a focused box does not reopen its list
// on the next tap, which a touch operator never meets.)
export async function dismissNameBox(page) {
  await page.touchscreen.tap(6, 500);
}

// Type a name into a LineupNameInput and commit it with Enter.
export async function typeIntoNameBox(input, name) {
  await input.tap();
  await input.fill(name);
  await input.press('Enter');
}

export async function panelSave(page, i) {
  await panelSide(page, i).getByRole('button', { name: 'Save lineup' }).tap();
  await expect(panelSide(page, i).getByRole('button', { name: 'Save lineup' })).toBeEnabled();
}

export async function closePanel(page) {
  await page.getByRole('button', { name: /✕ Close|Done/ }).first().tap();
  await expect(page.locator('[data-testid^="match-lineup-side-"]')).toHaveCount(0);
}

// ---------------------------------------------------------------- team sheet

// A bout row by its number column ("1".."N", or "DH" for the daihyosen).
export const boutRow = (root, n) => root.locator('.team-sub-match').filter({
  has: root.page().locator('.team-sub-match__pos-num', { hasText: new RegExp(`^${n}$`) }),
}).first();

export const rowSide = (row, side) => row.locator(`.team-sub-match__side--${side}`);
export const rowIppon = (row, side, waza = 'M') => rowSide(row, side).locator('.ipt-btn', { hasText: new RegExp(`^${waza}$`) }).first();
export const rowFusensho = (row, side) => rowSide(row, side).getByTestId('scoring-modal-fusensho-button');
export const rowTie = (row) => row.getByTestId('scoring-modal-tie-button');
export const rowSlots = (row, side) => row.locator(`.tsm-center-pts--${side} .editor-side__pt--filled`);
export const rowMiddle = (row) => row.locator('.team-sub-match__score');
export const rowNameBox = (row, side) => rowSide(row, side).locator('.lineup-name input');
export const rowMemberLabel = (row, side) => row.getByTestId(`team-sub-match-member-label-${side}`);

// Leave bout `n` a drawn bout (X in its centre). The Tie control is a toggle,
// so it is only tapped when the row does not already read X; the result is
// waited for, and the autosave with it.
export async function ensureTie(root, n) {
  const row = boutRow(root, n);
  await row.scrollIntoViewIfNeeded();
  await expect(syncPill(root)).toHaveText('Synced');
  if ((await rowMiddle(row).innerText()).trim() !== 'X') await rowTie(row).tap();
  await expect(rowMiddle(row)).toHaveText('X');
  await expect(syncPill(root)).toHaveText('Synced');
}

// The IV/PW band: { shiro: "IV: 1 · PW: 2", aka: ..., verdict }.
export async function teamTotals(root) {
  const stats = root.locator('.team-summary__stats');
  return {
    shiro: (await stats.nth(0).innerText()).trim(),
    aka: (await stats.nth(1).innerText()).trim(),
    verdict: (await root.locator('.team-summary__verdict').first().innerText()).trim(),
  };
}

export const syncPill = (root) => root.locator('.sync-pill__label').first();

// The whole sheet as one line per bout, "shiro marks | middle | aka marks",
// e.g. { 1: 'M|vs|', 2: '|vs|M', 3: '|X|' }, for the audit record.
export async function sheetState(root, bouts = 5) {
  const out = {};
  for (let n = 1; n <= bouts; n += 1) {
    const r = boutRow(root, n);
    if (!(await r.count())) continue;
    out[n] = `${(await rowSlots(r, 'shiro').allInnerTexts()).join('')}|${(await rowMiddle(r).innerText()).trim()}|${(await rowSlots(r, 'aka').allInnerTexts()).join('')}`;
  }
  return out;
}

// Start the Up next team match and return once the court has it running:
// the sheet shows its autosave pill only once the start has landed (before
// that it reads PRE-MATCH, and a strike tapped then is not saved).
export async function startUpNextTeam(page) {
  await page.locator('.shiaijo-upnext__card').getByRole('button', { name: 'Start match' }).tap();
  const ed = page.locator('.scoring-panel');
  await expect(boutRow(ed, 1)).toBeVisible();
  await expect(syncPill(ed)).toBeVisible();
  return ed;
}

// Award one strike on a bout row and wait for the autosave to land.
export async function boutIppon(root, n, side, waza = 'M') {
  const row = boutRow(root, n);
  const before = await rowSlots(row, side).count();
  await rowIppon(row, side, waza).tap();
  await expect(rowSlots(row, side)).toHaveCount(before + 1);
  await expect(syncPill(root)).toHaveText('Synced');
}

// The primary footer button in whatever state it is in.
export const finishBtn = (root) => root.locator('.score-nav__actions button.btn--primary').first();

// Finish in two taps and wait for the write to land.
export async function finishTeam(root) {
  const btn = finishBtn(root);
  await expect(btn).toHaveText(/^Finish( \+ Start Next →)?$/);
  await btn.tap();
  await expect(btn).toHaveText(/^Tap again to finish/);
  await btn.tap();
  await expect(root.locator('button', { hasText: /^(Tap again to finish|Saving…)/ })).toHaveCount(0, { timeout: 15000 });
}

// Tap-target size of a control, for the audit's >= 44px rule.
// Measured only under (pointer: coarse), where the 44px tap-floor rules apply:
// the measurement asserts it first.
export async function tapBox(locator) {
  const coarse = await locator.page().evaluate(() => matchMedia('(pointer: coarse)').matches);
  expect(coarse, 'tap targets are measured under (pointer: coarse)').toBe(true);
  const b = await locator.boundingBox();
  return b ? { w: Math.round(b.width), h: Math.round(b.height), ok: b.width >= 44 && b.height >= 44 } : null;
}
