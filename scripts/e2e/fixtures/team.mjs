// TEAM (non-kachinuki) competitions: naming team members, the three lineup entry
// points, and scoring a bout inside a team encounter.
//
//   the Lineups page      /admin/competition/:id/lineups (admin_lineup.jsx)
//                         writes one team's STARTING lineup, or, through its
//                         "Lineup for" select, one match's own lineup
//   the match panel       "Enter lineup" / "Lineup" on the shiaijo queue
//                         (admin_schedule_lineup.jsx), writes the MATCH lineup
//   the score sheet row   the name box on each bout row of the team editor
//                         (admin_scoring_team.jsx), writes the MATCH lineup
//
// The shared scoring helpers (fixtures/scoring.mjs) scope to the INDIVIDUAL
// board's `.sb-side--*` wrappers, which the team sheet does not have, so the
// team sheet's own selectors live here, for the kachinuki sheet as well
// (fixtures/kachinuki.mjs reads its bout rows through them), and so does the
// lineup panel. Every helper performs a real tap on a real control; none of
// them asserts app behaviour beyond "the tap landed", except Finish, which
// checks the write landed (fixtures/scoring.mjs finishLanded).
import { expect } from '@playwright/test';
import { EXPECT_TIMEOUT_MS } from './devices.mjs';
import { dwell, settled, tapOnSide } from './pace.mjs';
import { finishMatch } from './scoring.mjs';
import { inlineEditor, queueCaughtUpWith, sidesNow, upNextCard } from './shiaijo.mjs';

// FIK position names for a five-person team, in order; any other size uses
// bare numbers (admin_lineup.jsx positionsForSize).
export const FIK5 = ['Senpo', 'Jiho', 'Chuken', 'Fukusho', 'Taisho'];
export const positionLabels = (teamSize) => (teamSize === 5 ? FIK5 : Array.from({ length: teamSize }, (_, i) => String(i + 1)));

// ---------------------------------------------------------------- Lineups page

const overlineField = (page, text) => page.locator('label')
  .filter({ has: page.locator('.overline', { hasText: new RegExp(`^${text}$`, 'i') }) }).first();

export const lineupsForm = (page) => page.getByTestId('lineup-form-root');

// The Lineups page's own name for a team's lineup before any match has one of
// its own: what the "Lineup for" select offers first and the form's overline
// reads for it.
export const STARTING_LINEUP = 'Starting lineup';

// Open the Lineups page on `team` (its display name) and on the lineup the
// "Lineup for" select names: the team's "Starting lineup" (the default), or the
// label of one of its matches as the select lists it (e.g. "Pool A · Match 1").
export async function openLineups(page, id, { team, lineupFor = STARTING_LINEUP } = {}) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/lineups`);
  await expect(lineupsForm(page)).toBeVisible();
  if (team) {
    const sel = overlineField(page, 'Team').locator('select');
    await sel.selectOption({ label: team });
    await expect(lineupsForm(page).locator('h2')).toContainText(team);
  }
  if (lineupFor !== STARTING_LINEUP) {
    await overlineField(page, 'Lineup for').locator('select').selectOption({ label: lineupFor });
  }
  // The form's overline names the lineup it edits.
  await expect(lineupsForm(page).locator('.overline').first()).toHaveText(lineupFor);
  // The team members load separately from the lineup; wait for their list.
  await expect(lineupsForm(page).getByText('Team members', { exact: true })).toBeVisible();
}

// The choices of the Lineups page's "Lineup for" select, in order: "Starting
// lineup" first, then the team's matches by the labels the select shows.
export const lineupForOptions = async (page) => (await overlineField(page, 'Lineup for').locator('option').allInnerTexts())
  .map((t) => t.trim());

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

// Answer the open confirm dialog with its named button. The wait is on the
// dialog that was answered (its element handle), not on a re-resolving
// locator, which a second dialog opened by the answer would satisfy.
async function answerDialog(page, label) {
  const dialog = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();
  // A confirm ignores a tap inside the bounce window of its opening.
  await settled(dialog);
  const handle = await dialog.elementHandle();
  try {
    await dialog.getByRole('button', { name: label }).tap();
    await handle.waitForElementState('hidden', { timeout: EXPECT_TIMEOUT_MS });
  } finally {
    await handle.dispose();
  }
}

// Name a position's blank seeded slot through "+ Add new member…" and the
// "Name slot" confirm, the way the docs describe it.
export async function lineupsNameSlot(page, label, name) {
  const r = await lineupsStartAdd(page, label, name);
  if (r.dialog) await answerDialog(page, /^(Name slot|Add member)$/);
  await expect(lineupsSelect(page, label).locator('option:checked')).toContainText(name);
}

// The PUT every lineup save sends: a team's lineup of a round (the Lineups
// page) or of one match (the lineup panel, the score sheet row), api_client.jsx
// putTeamLineup / putMatchLineup. Armed BEFORE the tap that sends it. A timeout
// the caller never reached (the tap threw first) is not an unhandled rejection.
// `teamId` names the team whose lineup is being saved, and `matchId` the match
// whose own lineup it is (`null`: the team's starting lineup, round 0); a PUT
// for another team or lineup is not this save's answer. `matchId` undefined
// matches any match lineup of the team (the lineup panel does not show the id).
const LINEUP_PUT = /\/teams\/([^/]+)\/(lineups|match-lineups)\/([^/?]+)(\?|$)/;
function lineupPut(r, { teamId, matchId }) {
  if (r.method() !== 'PUT') return false;
  const m = LINEUP_PUT.exec(r.url());
  if (!m || decodeURIComponent(m[1]) !== teamId) return false;
  if (matchId === undefined) return m[2] === 'match-lineups';
  if (matchId === null) return m[2] === 'lineups' && decodeURIComponent(m[3]) === '0';
  return m[2] === 'match-lineups' && decodeURIComponent(m[3]) === matchId;
}
function lineupSaveAnswer(page, target) {
  const answered = page.waitForResponse((r) => lineupPut(r.request(), target), { timeout: EXPECT_TIMEOUT_MS });
  answered.catch(() => {});
  return answered;
}

// Tap Save lineup on the Lineups page and return once the server has answered
// the save with a success status. The "Lineup saved" toast is not the signal: it
// also opens "Lineup saved, but ..." warnings, and an older toast outlives it.
export async function lineupsSave(page) {
  // The save is of the team and lineup the two selects show right now.
  const teamId = await overlineField(page, 'Team').locator('select').inputValue();
  const matchId = (await overlineField(page, 'Lineup for').locator('select').inputValue()) || null;
  const answered = lineupSaveAnswer(page, { teamId, matchId });
  await lineupsForm(page).getByRole('button', { name: 'Save lineup' }).tap();
  const res = await answered;
  expect(res.ok(), `the lineup save was answered ${res.status()} ${res.url()}`).toBe(true);
}

// The option text currently selected at a position (e.g. "T1.2 Ito").
export const lineupsSelected = async (page, label) => (await lineupsSelect(page, label).locator('option:checked').innerText()).trim();

// ---------------------------------------------------------------- match panel

// The per-match lineup panel (MatchLineupPanel, admin_schedule_lineup.jsx), a
// dialog named "Lineup for this match", opened by "Enter lineup" on the
// shiaijo page's Up next card or the Scores tab. Its sides are found by index
// (0 = Shiro, left; 1 = Aka, right) or by the team name they show.
const lineupPanel = (page) => page.getByRole('dialog', { name: 'Lineup for this match' });
const panelSides = (page) => lineupPanel(page).locator('[data-testid^="match-lineup-side-"]');
export const panelSide = (page, i) => panelSides(page).nth(i);
export const panelSideOf = (page, teamName) => panelSides(page).filter({ hasText: teamName }).first();
export const panelInput = (page, i, label) => panelSide(page, i).getByRole('textbox', { name: `${label} player`, exact: true });
// Where the lineup a side shows comes from: "Lineup for this match", "Same as
// <match>" or "Starting lineup" (LineupSourceLine, lineup_draft.jsx).
export const sideSource = (side) => side.locator('[data-testid^="match-lineup-source-"]');
export const panelSource = (page, i) => sideSource(panelSide(page, i));
// The side's Save lineup, which stays disabled until the operator changes
// something on that side and reads "Saving…" while the write is out.
export const panelSaveButton = (page, i) => panelSide(page, i).getByRole('button', { name: /^(Save lineup|Saving…)$/ });

// Open the Up next card's "Enter lineup".
export async function openPanelFromUpNext(page) {
  await upNextCard(page).getByRole('button', { name: 'Enter lineup' }).tap();
  await expect(lineupPanel(page)).toBeVisible();
  await expect(panelSide(page, 1)).toBeVisible();
}

// page -> when the last name-list option was tapped. A pick from the list stamps
// the score sheet's bout list (stampTap(boutListTapRef), admin_scoring_team.jsx),
// and that list swallows every click inside it for TAP_BOUNCE_MS afterwards: the
// next option tap is a bounce of the pick's tap to the app, and a pick made that
// soon does nothing (the list stays open, no write is sent). A person's next pick
// is never that close, Playwright's is, so every option tap waits out the dwell
// since the previous one, timed from that tap (nothing in the page says the
// window has passed).
const optionTaps = new WeakMap();

// Tap one option of an open name list, at the operator's pace. The one owner of
// "tap a name-list option"; a journey that taps an option itself calls this.
export async function tapNameOption(option) {
  const page = option.page();
  const last = optionTaps.get(page);
  if (last !== undefined) await dwell(page, last);
  await option.tap();
  optionTaps.set(page, Date.now());
}

// Wait out the dwell since the last option tapNameOption made, for a journey's
// own next tap inside the team sheet's bout list (reopening a name box right
// after a pick), which the sheet ignores within that window.
export async function dwellAfterNameOption(page) {
  const last = optionTaps.get(page);
  if (last !== undefined) await dwell(page, last);
}

// Tap a LineupNameInput and choose the option containing `text`. Works on the
// panel and on the score sheet row alike (both render LineupNameInput). The
// box is tapped after the dwell since the last option tap, since on the score
// sheet that pick stamped the bout list, which ignores a tap inside it for
// the bounce window (a box tapped sooner never opens its list).
export async function pickFromNameBox(input, text) {
  await dwellAfterNameOption(input.page());
  await input.tap();
  const box = input.locator('xpath=ancestor::div[contains(concat(" ", normalize-space(@class), " "), " lineup-name ")][1]');
  const option = box.locator('.pmf__option').filter({ hasText: text }).first();
  await expect(option).toBeVisible();
  await tapNameOption(option);
}

// A name list left open (by the operator, or by a tap that landed on a name
// box). Close it with Escape on the box that owns the list: an open list takes
// the key and keeps it from the layer behind (LineupNameInput stops it there),
// and pressing on the owning input puts the key where the list is, whatever
// the focus was. A fixed tap on the page margin is not used: where it lands
// depends on the surface (on the shiaijo page it can be a queue row). Escape
// is pressed only while a list is drawn, since one that arrives with none
// closes the layer behind instead (the panel, or the editor). Returns whether
// one was open. Any LineupNameInput: the panel, the team sheet, the kachinuki
// sheet.
export async function closeNameList(page) {
  const options = page.locator('.pmf__option');
  const wasOpen = (await options.count()) > 0;
  if (wasOpen) {
    await page.locator('.lineup-name').filter({ has: options }).first().locator('input.pmf__input').press('Escape');
    await expect(options).toHaveCount(0);
  }
  return wasOpen;
}

// Type a name into a LineupNameInput and commit it with Enter.
export async function typeIntoNameBox(input, name) {
  await input.tap();
  await input.fill(name);
  await input.press('Enter');
}

// Tap one side's Save lineup and return once the save has been answered, then
// once the button is back from "Saving…" (disabled when the side was saved:
// nothing is left to save). By default the server must answer the save PUT with
// a success status. `refused: true` is for a save the panel refuses: it waits
// for the side's inline error instead (a refusal the panel makes itself sends
// no request), and the caller asserts its text.
export async function panelSave(page, i, { refused = false, matchId } = {}) {
  const side = panelSide(page, i);
  // The side's testid ends in its team's id (admin_schedule_lineup.jsx).
  const teamId = (await side.getAttribute('data-testid')).replace(/^match-lineup-side-/, '');
  const answered = refused ? null : lineupSaveAnswer(page, { teamId, matchId });
  if (refused) {
    // An alert from an earlier save would satisfy the wait below on its own.
    await expect(side.locator('.alert--error'), 'no refusal is showing before the tap').toHaveCount(0);
    const sent = [];
    const onRequest = (r) => { if (lineupPut(r, { teamId, matchId })) sent.push(r.url()); };
    page.on('request', onRequest);
    try {
      await panelSaveButton(page, i).tap();
      await expect(side.locator('.alert--error')).toBeVisible();
      // A refusal the panel makes itself sends nothing; give a stray write time to go out.
      await page.waitForTimeout(500);
      expect(sent, 'the refused save sent no lineup write').toEqual([]);
    } finally {
      page.off('request', onRequest);
    }
  } else {
    await panelSaveButton(page, i).tap();
    const res = await answered;
    expect(res.ok(), `the lineup save was answered ${res.status()} ${res.url()}`).toBe(true);
  }
  await expect(side.getByRole('button', { name: 'Save lineup' })).toBeVisible();
}

export async function closePanel(page) {
  await lineupPanel(page).getByRole('button', { name: /^(✕ Close|Done)$/ }).tap();
  await expect(lineupPanel(page)).toHaveCount(0);
}

// ---------------------------------------------------------------- team sheet

// A bout row by its number column ("1".."N", or "DH" for the daihyosen).
export const boutRow = (root, n) => root.locator('.team-sub-match').filter({
  has: root.page().locator('.team-sub-match__pos-num', { hasText: new RegExp(`^${n}$`) }),
}).first();

const rowSide = (row, side) => row.locator(`.team-sub-match__side--${side}`);
export const rowIppon = (row, side, waza = 'M') => rowSide(row, side).locator('.ipt-btn', { hasText: new RegExp(`^${waza}$`) }).first();
export const rowFusensho = (row, side) => rowSide(row, side).getByTestId('scoring-modal-fusensho-button');
export const rowTie = (row) => row.getByTestId('scoring-modal-tie-button');
export const rowSlots = (row, side) => row.locator(`.tsm-center-pts--${side} .editor-side__pt--filled`);
export const rowMiddle = (row) => row.locator('.team-sub-match__score');
export const rowNameBox = (row, side) => rowSide(row, side).locator('.lineup-name input');
export const rowMemberLabel = (row, side) => row.getByTestId(`team-sub-match-member-label-${side}`);

// Leave bout `n` a drawn bout (X in its centre). The Tie control is a toggle
// (a bout.decision tap, hikiwake or clear), so it is only tapped when the row
// does not already read X; the result is waited for, and the tap's answer
// with it (the sync pill reads the outbox).
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

// The sheet's sync pill. Every strike, Tie and fusensho on a running sheet is a
// tap queued in the device's outbox (bc-evts stage 5); "Synced" means the
// outbox is empty, so every tap this device made has been answered.
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
// the sheet shows its sync pill only once it holds the match running (before
// that it reads PRE-MATCH, and a strike tapped then is a draft, sent only by
// Start).
export async function startUpNextTeam(page) {
  const pair = await sidesNow(upNextCard(page).first());
  await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
  const ed = inlineEditor(page);
  await expect(boutRow(ed, 1)).toBeVisible();
  await expect(syncPill(ed)).toBeVisible();
  if (pair) await queueCaughtUpWith(page, pair);
  return ed;
}

// Award one strike on a bout row and return once the board shows it and the
// tap has been answered (the outbox is empty). `getRow` returns the row (a function, because the
// kachinuki sheet's live row is a different element after each Record bout);
// `key` names the bout for pacing, so a second strike for the same side of
// the same bout waits out the bounce window of the first (fixtures/pace.mjs).
// The one strike, for the team sheet (boutIppon) and the kachinuki sheet
// (fixtures/kachinuki.mjs awardBoutIppon) alike.
export async function ipponOnRow(root, getRow, key, side, waza = 'M') {
  const before = await rowSlots(getRow(), side).count();
  await tapOnSide(root.page(), `${key} ${side}`, rowIppon(getRow(), side, waza));
  await expect(rowSlots(getRow(), side)).toHaveCount(before + 1);
  await expect(syncPill(root)).toHaveText('Synced');
}

// One strike on bout `n` of the team sheet.
export const boutIppon = (root, n, side, waza = 'M') => ipponOnRow(root, () => boutRow(root, n), `bout ${n}`, side, waza);

// The primary footer button in whatever state it is in.
export const finishBtn = (root) => root.locator('.score-nav__actions button.btn--primary').first();

// Finish in two taps and return once the write has landed, failing if it did
// not: fixtures/scoring.mjs finishMatch, which reads the same signs on the
// team sheet as on the individual board.
export const finishTeam = (root) => finishMatch(root.page(), root);
