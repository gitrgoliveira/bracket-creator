// A SELF-RUN tournament as a competitor meets it: the public phone page opens
// a match from any of four doors, taps Report result, and scores it on the
// editor the page mounts (individual, naginata, team, kachinuki, engi). Every
// selector for those steps lives here, built on public.mjs (the doors),
// scoring.mjs, team.mjs, kachinuki.mjs and engi.mjs (the editors' own
// helpers, each taking the editor as a root where it can), so a journey about
// the public page reads as the steps a competitor takes.
//
// The tournament, its competitions, rosters and draws are made the way an
// organiser makes them, on a SECOND context (OPERATOR_DEVICE) through the
// wizard, the participants page and Generate draw (seedDrawReady). Starting and
// scoring are left to the public page: a draw-ready competition starts on its
// first recorded result.
//
// The public editors are OVERLAY dialogs (`.editor-modal`, EDITOR), never the
// court console's inline panel (`.scoring-panel`), so the helpers here pass
// EDITOR where the shared ones default to the inline editor, and the kachinuki
// sheet gets its own overlay-aware selectors (fixtures/kachinuki.mjs is the
// inline one's).
import { expect } from '@playwright/test';
import { EDITOR, finishMatch, ipponButton, settled, startMatch } from './scoring.mjs';
import { PUBLIC_DEVICE } from './devices.mjs';
import { armTwoTap, tapOnSide } from './pace.mjs';
import { createCompetition } from './wizard.mjs';
import { generateDraw, pasteRoster } from './competition.mjs';
import {
  competitionTab, homeNowRow, matchCard, openViewer, poolRow, recentResult, scheduleRow, upNextRow,
} from './public.mjs';
import * as Engi from './engi.mjs';
import * as Team from './team.mjs';

export { EDITOR, matchCard, settled };

// The first-run form's "Tournament type" pill, for enterTournament({ mode }).
export const SELF_RUN_MODE = 'Self-run';
// A tournament with enough shiaijo for each journey test to take one of its own.
export const SELF_RUN_CUP = { name: 'Self-run Cup', courts: 4, mode: SELF_RUN_MODE };

// ------------------------------------------------------------------ seeding --

// Make one competition through the wizard, paste its roster and generate the
// draw, all on the organiser's page `op`. Returns its id once the competition
// reads "Draw ready". `competition` is createCompetition's options; `roster`
// is its list of [name, dojo] pairs (a team competition's are [team, dojo]).
export async function seedDrawReady(op, { competition, roster }) {
  const id = await createCompetition(op, competition);
  await pasteRoster(op, id, roster);
  await generateDraw(op, id);
  return id;
}

// ------------------------------------------------------------- second phone --

// Another phone on the public page: a fresh context with the public device's
// shape, which the console policy listens to like the test's own page. Close
// `ctx` in a finally. The caller opens the page it wants on `page`.
export async function openSecondPhone(browser, baseURL) {
  const ctx = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
  const page = await ctx.newPage();
  return { ctx, page };
}

// ------------------------------------------------------------- the doors --

// The four doors into a match card (spec FR2), by the name a journey gives them.
//   home      the home page's Now and Up next rows
//   tab       the competition page's Bracket, Pools or League tab (`tab`)
//   overview  the competition page's Overview: Up next, Now and Recent results
//   schedule  the Schedule page
export const DOORS = ['home', 'tab', 'overview', 'schedule'];

// The row of a door naming both competitors, to tap. `shiro` and `aka` are
// the names the row shows; `id` is the competition (the tab and Overview
// doors are its own page); `tab` is 'Bracket' | 'Pools' | 'League' for the tab
// door; `recent` picks the Overview's Recent results row of a finished match
// instead of its Up next row. The page is navigated to the door first.
export async function doorRow(page, door, { id, shiro, aka, tab = 'Pools', recent = false }) {
  switch (door) {
    case 'home':
      await page.goto('/');
      await expect(page.locator('.viewer__body')).toBeVisible();
      return homeNowRow(page).or(upNextRow(page, { shiro, aka })).filter({ hasText: shiro }).filter({ hasText: aka }).first();
    case 'tab': {
      await openViewer(page, id);
      await competitionTab(page, tab).tap();
      // The bracket draws a match as a card button, a pool or league as a numbered row.
      return (tab === 'Bracket'
        ? page.locator('button.bc-match').filter({ hasText: shiro }).filter({ hasText: aka })
        : poolRow(page, { shiro, aka })).first();
    }
    case 'overview':
      await openViewer(page, id);
      return (recent ? recentResult(page, { shiro, aka }) : upNextRow(page, { shiro, aka })).first();
    case 'schedule':
      await page.goto('/schedule');
      await expect(page.locator('.viewer__body')).toBeVisible();
      return scheduleRow(page, { shiro, aka }).first();
    default:
      throw new Error(`doorRow: no door "${door}" (one of ${DOORS.join(', ')})`);
  }
}

// Open the match card through a door and return it once it shows.
export async function openMatchCard(page, door, which) {
  const row = await doorRow(page, door, which);
  await expect(row, `the ${door} door lists the match`).toBeVisible();
  await row.tap();
  await expect(matchCard(page)).toBeVisible();
  return matchCard(page);
}

// ---------------------------------------------------------- the match card --

// What a card says of its match: Report result while a result can be entered,
// "Result reported" (with the sentence about the organizer) once finished, and
// the line that a result entered on this phone is waiting to be sent.
export const reportResultButton = (page) => matchCard(page).getByRole('button', { name: 'Report result', exact: true });
export const resultReportedBadge = (page) => matchCard(page).getByText('Result reported', { exact: true });
export const contactOrganizerLine = (page) => matchCard(page).getByText('Contact the organizer to correct this result.', { exact: true });
export const resultWaitingLine = (page) => matchCard(page).getByTestId('result-unsent');

// Tap Report result and return the editor it opens (the overlay, EDITOR).
export async function reportResult(page) {
  await reportResultButton(page).tap();
  const editor = publicEditor(page);
  await expect(editor).toBeVisible();
  return editor;
}

// Close the match card with its Close control.
export async function closeMatchCard(page) {
  await matchCard(page).getByRole('button', { name: 'Close' }).first().tap();
  await expect(matchCard(page)).toHaveCount(0);
}

// ----------------------------------------------------------- public editors --

export const publicEditor = (page) => page.locator(EDITOR).first();

// What no public editor offers (spec FR3, FR8): the decisions that stay with
// the organiser, and the match history disclosure. Each count must be 0 on
// every editor; a journey asserts them on the editor it has open.
const DECISION_TEST_IDS = [
  'scoring-modal-kiken-voluntary-button', 'scoring-modal-kiken-injury-button', 'scoring-modal-fusenpai-button',
  'scoring-modal-fusensho-button', 'team-daihyosen-hantei-arm',
];
const DECISION_WORDS = /kiken|fusenpai|fusensho|hantei/i;
export async function offeredDecisions(page) {
  const ed = publicEditor(page);
  const found = [];
  for (const id of DECISION_TEST_IDS) if (await ed.getByTestId(id).count()) found.push(id);
  const texts = await ed.getByRole('button').allInnerTexts();
  for (const t of texts) if (DECISION_WORDS.test(t)) found.push(t.trim());
  return found;
}
export const matchHistory = (page) => publicEditor(page).getByTestId('match-history');

// Individual and naginata: the board's Start match (the editor stays open and
// the match runs), an ippon button and the two-tap Finish.
export const startPublicMatch = (page) => startMatch(page);
export const publicIppon = (page, side, waza = 'M') => ipponButton(page, side, waza, EDITOR);
export const suneButton = (page, side) => publicIppon(page, side, 'S');

// Finish on the public editor in two taps. A landed Finish closes the editor
// and the match card with it; returns once the card is gone.
export async function finishPublicMatch(page) {
  await finishMatch(page, EDITOR);
  await expect(publicEditor(page)).toHaveCount(0);
  await expect(matchCard(page)).toHaveCount(0);
}

// Team (regular): the sheet's rows and Finish, from the shared team helpers
// rooted on the overlay.
export const teamSheet = (page) => publicEditor(page);
export const teamBoutIppon = (page, n, side, waza = 'M') => Team.boutIppon(teamSheet(page), n, side, waza);
export const teamTotals = (page) => Team.teamTotals(teamSheet(page));
export const teamFinish = (page) => Team.finishTeam(teamSheet(page));
export const teamBoutRow = (page, n) => Team.boutRow(teamSheet(page), n);
// The representative bout's controls (a level knockout team match while running).
export const addRepresentativeBout = (page) => teamSheet(page).getByTestId('scoring-modal-daihyosen-button');
export const removeRepresentativeBout = (page) => teamSheet(page).getByTestId('team-daihyosen-remove');

// Kachinuki: the overlay's sheet. fixtures/kachinuki.mjs selects the inline
// panel, so the overlay gets its own handles on the same markup.
export const kachinukiSheet = (page) => publicEditor(page).filter({ has: page.locator('.team-bouts-scroll') });
export const kachinukiCurrentBout = (page) => kachinukiSheet(page)
  .locator('.team-sub-match:not(.team-sub-match--readonly):not(.team-sub-match--correcting)').first();
export const kachinukiDoneRows = (page) => kachinukiSheet(page).locator('.team-sub-match--readonly');
export const kachinukiRecordBoutButton = (page) => kachinukiSheet(page).getByRole('button', { name: /^(Record bout|Saving…)$/ }).first();
export const kachinukiEndMatchButton = (page) => kachinukiSheet(page).getByTestId('kachinuki-end-match-button');
export const kachinukiEnchoButton = (page) => kachinukiSheet(page).getByTestId('kachinuki-encho-button');

// One ippon on the current kachinuki bout; returns once the board shows it
// and the outbox is empty.
export const kachinukiBoutIppon = (page, side, waza = 'M') => Team.ipponOnRow(
  kachinukiSheet(page), () => kachinukiCurrentBout(page), 'current bout', side, waza,
);

// Record bout on the current kachinuki bout: one paced tap; returns once the
// bout became a read-only row. The key is the one Record bout and Remove this
// bout share (a single place on the sheet).
export async function kachinukiRecordBout(page) {
  const before = await kachinukiDoneRows(page).count();
  await tapOnSide(page, 'bout change', kachinukiRecordBoutButton(page));
  await expect(kachinukiDoneRows(page)).toHaveCount(before + 1);
}

// End match: the two-tap guard. Returns the armed label the first tap showed.
export async function kachinukiEndMatch(page) {
  const btn = kachinukiEndMatchButton(page);
  const armed = await armTwoTap(btn, btn.filter({ hasText: /^Tap again/ }));
  const label = (await armed.innerText()).trim();
  await armed.tap();
  return label;
}

// Engi: the flag editor on the overlay, the shared engi helpers with EDITOR as
// their root.
export const engiFlags = (page, flags) => Engi.enterFlags(page, flags, EDITOR);
export const engiSave = (page) => Engi.saveEngiResult(page, EDITOR);
export const engiFlagCount = (page, side) => Engi.flagCount(page, side, EDITOR);
export const engiSubmitButton = (page) => Engi.engiSubmit(page, EDITOR);
export const engiEditorOpen = (page) => Engi.engiEditor(page, EDITOR);

// ---------------------------------------------------------- going offline --

// The corner count ("1 result not sent") and the list it opens (public-held,
// held_writes_panel.jsx). Offline, a result is kept on the phone and the list
// shows it waiting for the connection with no Discard.
export const unsentCorner = (page) => page.getByTestId('public-held');
export const unsentList = (page) => page.getByTestId('held-writes');
export const unsentItems = (page) => page.getByTestId('held-write');
export const unsentDiscard = (page) => page.getByTestId('held-write-discard-one');

export async function openUnsentList(page) {
  await unsentCorner(page).tap();
  await expect(unsentList(page)).toBeVisible();
  return unsentList(page);
}

// Take the phone's context offline. The console policy allows the failed /api
// requests it logs only while offline (test.mjs, whileOffline); call
// comeBackOnline in a finally so a failed test leaves no offline context behind.
export async function goOffline(ctx) {
  await ctx.setOffline(true);
}
export async function comeBackOnline(ctx) {
  await ctx.setOffline(false);
}
