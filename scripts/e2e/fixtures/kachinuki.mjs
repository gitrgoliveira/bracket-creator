// The kachinuki (winner stays on) score sheet as the court operator drives it
// on /admin/shiaijo/:court: the per-match lineup panel, the current bout's
// ippon buttons, Tie, Encho, Record bout, End match, the fought bouts above
// the current one, and the correction doors (tap a fought bout, Reopen match,
// the court-busy requeue panel, Remove this bout).
//
// Selectors follow admin_scoring_team.jsx and admin_schedule_lineup.jsx. The
// individual board's helpers (fixtures/scoring.mjs) do not fit here: a team
// bout's sides are `.team-sub-match__side--<colour>`, not `.sb-side--<colour>`.
// The bout row's controls and the lineup panel are the team sheet's
// (fixtures/team.mjs owns their selectors); this file adds what is kachinuki's
// own and names those for the current bout. Every tap goes through
// Locator.tap(), so the coarse-pointer hit test decides what is hit, as it
// does for a thumb.
import { expect } from '@playwright/test';
import { armTwoTap, tapOnSide } from './pace.mjs';
import { EXPECT_TIMEOUT_MS } from './devices.mjs';
import { clearMarks } from './scoring.mjs';
import { inlineEditor, sides, upNextCard } from './shiaijo.mjs';
import * as T from './team.mjs';

export const editor = (page) => inlineEditor(page).filter({ has: page.locator('.team-bouts-scroll') }).first();

// ---------------------------------------------------------------- lineup --

// The per-match lineup panel (fixtures/team.mjs): opened from the Up next
// card, a side found by the team name it shows, where its lineup comes from,
// and Close.
export const openLineupFromUpNext = T.openPanelFromUpNext;
export const lineupSide = T.panelSideOf;
export const lineupSource = T.sideSource;
export const closeLineupPanel = T.closePanel;

// Name every position of ONE team's lineup by typing (a typed name over a
// blank numbered slot names that slot's member, bc-dnst), then Save lineup.
// `teamName` picks the side editor by its heading text. Returns once the
// save has landed (the side reads its lineup as this match's own). `save:
// false` types the names and leaves the side unsaved.
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
  // Saved: the side shows the lineup as its own match's, and Save lineup waits
  // for the next change.
  await expect(lineupSource(side)).toHaveText('Lineup for this match');
  return side;
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

// A bout row's controls, the team sheet's (fixtures/team.mjs): an ippon
// button (`side` is 'shiro', left, the match's sideB, or 'aka', right,
// sideA), the filled centre slots of one side, and Tie.
export const boutIpponButton = T.rowIppon;
export const boutFilled = T.rowSlots;
export const tieButton = T.rowTie;
export const recordBoutButton = (page) => editor(page).getByRole('button', { name: /^(Record bout|Saving…)$/ }).first();
export const endMatchButton = (page) => editor(page).getByTestId('kachinuki-end-match-button');
export const enchoButton = (page) => editor(page).getByTestId('kachinuki-encho-button');
// Takes one overtime period back; offered beside Encho once the bout is in overtime.
export const enchoUndoButton = (page) => editor(page).getByTestId('kachinuki-encho-undo-button');
export const removeBoutButton = (page) => editor(page).getByTestId('kachinuki-remove-bout-button');
export const reopenButton = (page) => page.getByTestId('kachinuki-reopen-button');

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
export const boutMiddle = async (row) => (await T.rowMiddle(row).first().innerText()).trim();

// Tap one side's filled marks off a bout row until `keep` are left (the
// bounded clearMarks, fixtures/scoring.mjs); returns the taps made.
export const clearBoutMarks = (row, side, opts) => clearMarks(boutFilled(row, side), opts);

// Award one ippon on the current bout and return once the board shows it and
// the tap has been answered (fixtures/team.mjs ipponOnRow, the one strike the
// team sheet shares). A second ippon on the same side waits out the bounce
// window of the first (fixtures/pace.mjs).
export const awardBoutIppon = (page, side, waza = 'M') => T.ipponOnRow(editor(page), () => currentBout(page), 'current bout', side, waza);

// The guard key Record bout and Remove this bout share: the two sit in one
// place and the sheet takes a bounce of either through one guard
// (admin_scoring_team.jsx boutChangeTap("bout"), tap_guard.jsx acceptTap), so
// a tap on one within the bounce window of a tap on the other is ignored. A
// journey that taps either by other means waits it out with
// pace.mjs dwellOnSide(page, BOUT_CHANGE).
export const BOUT_CHANGE = 'bout change';

// Record bout: one tap, paced against the last Record bout this page made.
// Returns once the bout has become a read-only row (the server appended the
// next pairing, or the tie retired both).
export async function recordBout(page) {
  const before = await doneRows(page).count();
  await tapOnSide(page, BOUT_CHANGE, recordBoutButton(page));
  await expect(doneRows(page)).toHaveCount(before + 1, { timeout: EXPECT_TIMEOUT_MS });
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
  const armed = await armTwoTap(btn, btn.filter({ hasText: /^Tap again/ }));
  const label = (await armed.innerText()).trim();
  await armed.tap();
  return label;
}

// ------------------------------------------------------------ pool play --

// Start the Up next kachinuki encounter from its card and return the pairing
// (Shiro, Aka team names) once the sheet is running. The individual board's
// startUpNext (fixtures/shiaijo.mjs) waits for `.sb-side--*`, which this sheet
// does not have.
export async function startUpNextKachinuki(page) {
  const pair = await sides(upNextCard(page));
  await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
  await expect(recordBoutButton(page)).toBeVisible();
  return pair;
}

// The line that stands where the current bout was once Record bout took a
// team's last fighter off ("Aka has no fighters left. End match finishes the
// encounter.").
export const noPairingLine = (page) => editor(page).getByTestId('kachinuki-no-pairing');

// The representative bout (daihyosen) offer. A kachinuki sheet never carries
// it (admin_scoring_team.jsx hides the add affordance under isKachinuki), so a
// journey asserts both are absent.
export const daihyosenOffer = (page) => editor(page).getByTestId('scoring-modal-daihyosen-button');
export const daihyosenControls = (page) => editor(page).locator('.daihyosen-controls');
