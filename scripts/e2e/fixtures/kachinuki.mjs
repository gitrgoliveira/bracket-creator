// The kachinuki (winner stays on) score sheet as the court operator drives it
// on /admin/shiaijo/:court: the per-match lineup panel, the current bout's
// ippon buttons, Tie, Encho, Record bout, End match, the fought bouts above
// the current one, and the correction doors (tap a fought bout, Reopen match,
// the court-busy requeue panel, Remove this bout).
//
// Selectors follow admin_scoring_team.jsx and admin_schedule_lineup.jsx. The
// individual board's helpers (fixtures/scoring.mjs) do not fit here: a team
// bout's sides are `.team-sub-match__side--<colour>`, not `.sb-side--<colour>`.
// Every tap goes through Locator.tap(), so the coarse-pointer hit test decides
// what is hit, as it does for a thumb.
import { expect } from '@playwright/test';
import { INLINE_EDITOR } from '../../screenshots/lib/editor.mjs';

// Does the page still match (pointer: coarse)? Measured on this journey: after
// a fullPage screenshot (fixtures/shots.mjs with { fullPage: true }) the
// operator page no longer matches, so every tap-floor rule in styles.css
// silently switches off for the rest of the test. Put it back the way
// fixtures/test.mjs does (CDP touch emulation, which takes effect on the next
// document, hence the reload) and report whether it had been lost, so the
// audit can say which pointer a measurement was made at.
export async function ensureCoarse(page) {
  const coarse = () => page.evaluate(() => matchMedia('(pointer: coarse)').matches);
  if (await coarse()) return { coarseLost: false };
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 });
  await page.reload();
  if (!(await coarse())) throw new Error('page is not (pointer: coarse) even with CDP touch emulation');
  return { coarseLost: true, restoredBy: 'CDP Emulation.setTouchEmulationEnabled + reload' };
}

export const editor = (page) => page.locator(INLINE_EDITOR).filter({ has: page.locator('.team-bouts-scroll') }).first();

// ---------------------------------------------------------------- lineup --

// The per-match lineup panel (MatchLineupPanel, the "Enter lineup" button on
// the Up next card). It is a fixed overlay with no dialog role, found by its
// heading.
export const lineupPanel = (page) => page.locator('div').filter({
  has: page.getByRole('heading', { name: 'Lineup for this match' }),
}).filter({ has: page.getByRole('button', { name: /Close|Done/ }) }).last();

// Open the lineup panel from the Up next card.
export async function openLineupFromUpNext(page) {
  await page.locator('.shiaijo-upnext__card').getByRole('button', { name: 'Enter lineup' }).tap();
  await expect(page.getByRole('heading', { name: 'Lineup for this match' })).toBeVisible();
}

// Name every position of ONE team's lineup by typing (a typed name over a
// blank numbered slot names that slot's member, bc-dnst), then Save lineup.
// `teamName` picks the side editor by its heading text. Returns once the
// save has landed (the button leaves "Saving…"). `save: false` types the
// names and leaves the side unsaved.
export const lineupSide = (page, teamName) => page.locator('[data-testid^="match-lineup-side-"]').filter({ hasText: teamName }).first();

export async function typeLineup(page, teamName, names, { save: doSave = true } = {}) {
  const side = lineupSide(page, teamName);
  await expect(side).toBeVisible();
  const inputs = side.locator('input.pmf__input');
  await expect(inputs).toHaveCount(names.length);
  for (let i = 0; i < names.length; i += 1) {
    const box = inputs.nth(i);
    await box.tap();
    await box.fill(names[i]);
    await box.press('Enter');
    await expect(box).toHaveValue(names[i]);
  }
  if (!doSave) return side;
  const save = side.getByRole('button', { name: /^(Save lineup|Saving…)$/ });
  await save.tap();
  await expect(side.getByRole('button', { name: 'Save lineup' })).toBeEnabled();
  return side;
}

export async function closeLineupPanel(page) {
  await page.getByRole('button', { name: /^(✕ Close|Done)$/ }).first().tap();
  await expect(page.getByRole('heading', { name: 'Lineup for this match' })).toHaveCount(0);
}

// ----------------------------------------------------------------- bouts --

// Fought bouts, collapsed to read-only rows above the current one.
export const doneRows = (page) => editor(page).locator('.team-sub-match--readonly');
// The live bout: the one editable row that is not a fought bout reopened for
// correction.
export const currentBout = (page) => editor(page)
  .locator('.team-sub-match:not(.team-sub-match--readonly):not(.team-sub-match--correcting)').first();
// A fought bout opened for correction.
export const correctingBout = (page) => editor(page).locator('.team-sub-match--correcting').first();

// An ippon button on a bout row. `side` is 'shiro' (left, the match's sideB)
// or 'aka' (right, sideA).
export const boutIpponButton = (row, side, waza = 'M') => row
  .locator(`.team-sub-match__side--${side} .ipt-btn`, { hasText: new RegExp(`^${waza}$`) }).first();

// The filled centre slots of one side of a bout row.
export const boutFilled = (row, side) => row.locator(`.tsm-center-pts--${side} .editor-side__pt--filled`);

export const tieButton = (row) => row.getByTestId('scoring-modal-tie-button');
export const recordBoutButton = (page) => editor(page).getByRole('button', { name: /^(Record bout|Saving…)$/ }).first();
export const endMatchButton = (page) => editor(page).getByTestId('kachinuki-end-match-button');
export const enchoButton = (page) => editor(page).getByTestId('kachinuki-encho-button');
export const removeBoutButton = (page) => editor(page).getByTestId('kachinuki-remove-bout-button');
export const reopenButton = (page) => page.getByTestId('kachinuki-reopen-button');
export const syncPill = (page) => editor(page).locator('.sync-pill__label').first();

// The two fighters on a bout row. The live row carries a name box per side;
// a fought row carries static text. The box shows the name as its value
// while closed, but while its list is open the value is the typed QUERY and
// the name moves to the placeholder (LineupNameInput in
// admin_scoring_shared.jsx: `value={open ? query : value}`, `placeholder=
// {value || "Add player…"}`), so an empty value falls back to the
// placeholder.
export async function boutNames(row) {
  const read = async (side) => {
    const cell = row.locator(`.team-sub-match__side--${side} .tsm-name`).first();
    const input = cell.locator('input.pmf__input');
    if (await input.count()) {
      const v = (await input.inputValue()).trim();
      if (v) return v;
      const ph = ((await input.getAttribute('placeholder')) || '').trim();
      return ph === 'Add player…' ? '' : ph;
    }
    return (await cell.locator('.tsm-name__static').first().innerText()).trim();
  };
  return { shiro: await read('shiro'), aka: await read('aka') };
}

// Who a fought row marks as the winner ('shiro' | 'aka' | ''), read from the
// winner class on the name.
export async function doneRowWinner(row) {
  if (await row.locator('.team-sub-match__side--shiro .tsm-name__static--win').count()) return 'shiro';
  if (await row.locator('.team-sub-match__side--aka .tsm-name__static--win').count()) return 'aka';
  return '';
}

// The centre mark of a row (vs / X / (E)).
export const boutMiddle = async (row) => (await row.locator('.team-sub-match__score').first().innerText()).trim();

// Award one ippon on the live bout and wait for the board to show it.
export async function awardBoutIppon(page, side, waza = 'M') {
  const row = currentBout(page);
  const before = await boutFilled(row, side).count();
  await boutIpponButton(row, side, waza).tap();
  await expect(boutFilled(currentBout(page), side)).toHaveCount(before + 1);
}

// Record bout: one tap. Returns once the bout has become a read-only row (the
// server appended the next pairing, or the tie retired both).
export async function recordBout(page) {
  const before = await doneRows(page).count();
  await recordBoutButton(page).tap();
  await expect(doneRows(page)).toHaveCount(before + 1, { timeout: 15_000 });
  await expect(recordBoutButton(page)).not.toHaveText('Saving…');
}

// Win the live bout 2-0 (two men) for `side`, then Record bout.
export async function winBout(page, side) {
  await awardBoutIppon(page, side, 'M');
  await awardBoutIppon(page, side, 'K');
  await recordBout(page);
}

// End match: the two-tap guard. Returns the armed label the first tap showed.
export async function endMatch(page) {
  const btn = endMatchButton(page);
  await btn.tap();
  await expect(btn).toHaveText(/^Tap again/);
  const armed = (await btn.innerText()).trim();
  await btn.tap();
  return armed;
}
