// A LEAGUE competition (everyone fights everyone, one table): its admin Pools
// page (/admin/competition/:id/pools, admin_pools.jsx), its league settings,
// its public League and Awards tabs, and seeding one through the interface.
//
// The surfaces this file owns:
//
//   the league table      LeagueStandingsViewer (viewer_standings.jsx), the SAME
//                         component on admin Pools, the court console's context
//                         panel and the public League tab, so one reader serves
//                         them all: the rank cell (td.pool-standings__draw-pos,
//                         "3" or "3*" when a chusen set it), the name, the
//                         figures (W L T IV IL IT PW PL for a team, W L D PW PL
//                         for an individual), the DH badge, the heading
//                         ("Standings" / "Final standings"), "N/M matches" and
//                         the Winner badge
//   the numbered rows     PoolNumberedMatchRow under the table: one button per
//                         league match, Shiro on the left, Aka on the right; a
//                         tap opens the overlay score editor (EDITOR)
//   the matrix cell       LeagueMatrix (viewer_standings.jsx). Today it is drawn
//                         only inside PoolsViewer, for an INDIVIDUAL pool; no
//                         league surface renders it (LeagueStandingsViewer has no
//                         matrix), so on a league `leagueMatrix(page)` counts 0.
//                         The readers are here so a journey can record that and
//                         use the cell wherever it is drawn
//   the tie-break banner  "Tie-breaker required" (team league, status pools):
//                         one group per tie at a place that cannot be shared
//                         (1st and 2nd, and 3rd when two joint 3rd places are
//                         off), Run tie-breaker (its Remove unscored
//                         tie-breaker button is read, not tapped)
//   the drawn-bout notice above the table for a tie-breaker saved as a draw,
//                         with Open bout and Fight it again (built)
//   the lots              the team "Chusen (drawing lots) required" panel, the
//                         individual "A tie-breaker round left a tie" notice
//                         (another round, or Draw lots instead) and the
//                         "... recorded" panel: read, and entered (recordLots)
//   the settings          the "Award two joint 3rd places" checkbox on
//                         /admin/competition/:id/settings; it locks once a draw
//                         exists, so it is set before Generate draw. Every
//                         league breaks its ties at 1st to 3rd: no other band
//                         is offered (RETIRED names the controls that went)
//   the public page       the League tab, the Overview's Winner badge, the
//                         Awards tab and its podium
//   a backup copy         a competition backed up, its archive rewritten
//                         (config.md, pool-matches.csv) and restored as a copy:
//                         the one door to a league an earlier release saved
//
// The drawn-bout notice is built and found by its test id and the wording TEXT
// spells. The controls the league review built (the finished-league warning,
// the lots entry) are found by role and the visible text the plan fixed word
// for word (TEXT below).
// Readers and confirm helpers READ what is on screen and return it; the
// journeys assert.
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { expect } from '@playwright/test';
import { EXPECT_TIMEOUT_MS } from './devices.mjs';
import { settled as paceSettled } from './pace.mjs';
import { FIGURES, INDIVIDUAL_FIGURES } from './pools.mjs';
import { competitionTab, openViewer } from './public.mjs';
import { EDITOR, INLINE_EDITOR, awardIppon, finishButton, finishMatch, running, startMatch } from './scoring.mjs';
import { completedRowFor, sides, upNextCard } from './shiaijo.mjs';
import { editorSides, ensureRunning, inlineRunning } from './individual.mjs';
import { boutIppon, boutRow, ensureTie, finishTeam, teamTotals } from './team.mjs';
import { createCompetition } from './wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from './competition.mjs';
import { chooseBackup, confirmWithPassword, openBackupPage, planRow, restoreButton } from './organiser.mjs';
import { readZip, writeZip } from './archive.mjs';
import { escape } from './text.mjs';

// The words on screen: the controls' labels and the wording the league review
// fixed for its confirms and refusals.
export const TEXT = {
  bannerTitle: 'Tie-breaker required',
  // The banner's rule, in its description: which places a tie-breaker settles.
  bannerRule: 'A tie at 1st or 2nd place (and at 3rd place when joint 3rd places are off) needs a tie-breaker with a winner.',
  chusenTitle: 'Chusen (drawing lots) required',
  chusenRecordedTitle: 'Chusen (drawing lots) recorded',
  // The individual notice once a tie-breaker round leaves a tie (a circle, or
  // both sides of the tie-breaker withdrew earlier).
  circleTitle: 'A tie-breaker round left a tie',
  anotherRound: 'Run another round',
  drawLotsInstead: 'Draw lots instead',
  recordLots: 'Record chusen result',
  run: 'Run tie-breaker',
  remove: 'Remove unscored tie-breaker',
  cancel: 'Cancel',
  // A tie-breaker whose two sides both withdrew earlier: its row's note and
  // the one action it offers (ineligible_match.jsx).
  bothBarred: 'Both withdrew earlier: neither can fight this match.',
  recordDrawn: 'Record as drawn (neither can fight)',
  // Once it is drawn, both ways on, neither forced (operator ruling
  // 2026-11-16): the drawn-bout notice's choice, and the console's reason line.
  barredDrawChoice: 'Fight it again, or enter the lots below. Fought again, it waits on its court until both can fight; while they cannot, the court can record it as drawn again, which offers the lots again.',
  barredReason: 'Neither can fight: record it as drawn. The Pools tab then offers to fight it again or enter the lots.',
  fightAgain: 'Fight it again',
  // The warning before a result on a finished league is changed.
  finishedLeague: 'This league has finished. Changing this result can change the final table and podium. If it leaves a new tie at a podium place, the league goes back to match play to settle it.',
  changeAnyway: 'Change it anyway',
  // built: the Pools notice on a saved draw: its title, then its description, as
  // drawnBoutNotices reads them. A league names no pool and its table keeps the
  // two tied.
  drawnBout: (a, b, what = 'daihyosen') => `A ${what} was saved as a draw ${a} and ${b} are still tied, so the table keeps them tied. Fight it again, or open it to decide it by encho or hantei.`,
  openBout: 'Open bout',
  // settings
  twoThirdPlaces: 'Award two joint 3rd places',
};

// The league controls the tie rule retired (only 3rd can be shared): no page
// may offer them again. A journey asserts each counts 0.
export const RETIRED = {
  tiebreakTop: 'Break ties for top',
  sharedRanks: /Accept shared ranks|Undo shared ranks|Shared ranks accepted/,
};

// How many of the retired league controls the page shows (expected 0).
export async function retiredControls(page) {
  return {
    tiebreakTop: await page.getByText(RETIRED.tiebreakTop).count(),
    sharedRanks: await page.getByText(RETIRED.sharedRanks).count(),
  };
}

const exact = (s) => new RegExp(`^${escape(s)}$`);
// A `has:` locator must be built from the page, never chained off the root.
const pageOf = (root) => (typeof root.page === 'function' ? root.page() : root);
export const squash = (t) => (t || '').replace(/\s+/g, ' ').trim();
// One key for a pairing, whichever side each name fights on.
export const pairKey = (a, b) => [a, b].sort().join(' | ');

// ------------------------------------------------------------------- seeding

// Create a league through the wizard, paste the roster, draw and start it, and
// return its id. `entrants` are [name, dojo] pairs (teams for kind 'team',
// "Name 1 - Name 2" pairs for `engi`, single names for `engi` with `engiSolo`).
// `twoThirdPlaces` is the wizard's
// checkbox, set before the draw, which locks it. `draw: false` stops after the
// roster, `start: false` after the draw.
export async function seedLeague(page, {
  name, kind = 'team', entrants, teamSize = 3, teamMatchType = 'fixed', courts = ['A'], numberPrefix,
  twoThirdPlaces, engi, engiSolo, draw = true, start = true,
} = {}) {
  const team = kind === 'team';
  const id = await createCompetition(page, {
    name, kind, format: 'league', courts, numberPrefix, twoThirdPlaces, engi, engiSolo,
    ...(team ? { teamSize, teamMatchType } : {}),
  });
  await pasteRoster(page, id, entrants);
  if (!draw) return id;
  await generateDraw(page, id);
  if (start) await startCompetition(page, id);
  return id;
}

// ------------------------------------------------------------------ settings

export async function openLeagueSettings(page, id) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/settings`);
  await expect(page.getByText('Competition settings', { exact: true })).toBeVisible();
}

export const twoThirdPlacesBox = (page) => page.getByRole('checkbox', { name: TEXT.twoThirdPlaces });

// What the league settings say: the "Award two joint 3rd places" checkbox
// (shown, checked, locked, and the hint beside it, the settings copy F9 reads),
// and how many of the retired league controls the page still shows (RETIRED,
// expected 0).
export async function readLeagueSettings(page) {
  const box = twoThirdPlacesBox(page);
  await expect(page.getByRole('button', { name: 'Save changes' }).first()).toBeVisible();
  const out = { twoThirdPlaces: { shown: false }, retired: await retiredControls(page) };
  if (await box.count()) {
    const hint = box.locator('xpath=ancestor::div[contains(concat(" ", normalize-space(@class), " "), " field ")][1]').locator('.field__hint');
    out.twoThirdPlaces = {
      shown: true,
      checked: await box.isChecked(),
      locked: await box.isDisabled(),
      hint: (await hint.count()) ? squash(await hint.first().innerText()) : '',
    };
  }
  return out;
}

// Tick or untick "Award two joint 3rd places" (not saved until saveSettings).
export async function setTwoThirdPlaces(page, on) {
  const box = twoThirdPlacesBox(page);
  if ((await box.isChecked()) !== on) await box.tap();
  await expect(box).toBeChecked({ checked: on });
}

// Tap "Save changes" (the page has two, above and below the form; either saves)
// and return once the settings PUT has been answered with a success status.
export async function saveSettings(page, id) {
  const answered = page.waitForResponse((r) => r.request().method() === 'PUT'
    && new URL(r.url()).pathname === `/api/competitions/${id}`, { timeout: EXPECT_TIMEOUT_MS });
  answered.catch(() => {});
  await page.getByRole('button', { name: 'Save changes' }).first().tap();
  const res = await answered;
  expect(res.ok(), `the settings save was answered ${res.status()} ${res.url()}`).toBe(true);
  return res;
}

// --------------------------------------------------------------- league table

// The league's table block on any page that shows one (admin Pools, the public
// League tab, the court console's context panel).
export const leagueTable = (root) => root.locator('.pool').filter({
  has: pageOf(root).locator('.pool__table-caption', { hasText: /^Ranked by standings$/ }),
});

// The admin Pools page of a league, once its table is drawn.
export async function openLeague(page, id) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/pools`);
  await expect(leagueTable(page)).toBeVisible();
}

// The public viewer's League tab (a league has no Pools tab).
export async function openViewerLeague(page, id) {
  await openViewer(page, id);
  await competitionTab(page, 'League').tap();
  await expect(leagueTable(page)).toBeVisible();
}

// A table row by the name it shows (whole name, so "Team A" never matches
// "Team AB").
export const leagueRow = (root, name) => leagueTable(root).locator('table.pool__table tbody tr').filter({
  has: pageOf(root).locator('.pool__player-name .numbered-name__text', { hasText: exact(name) }),
});

// One row as data: rank (a number; NaN when the cell is empty), overridden
// (the "*" a chusen leaves), name, number, the figures by name, and whether
// the row carries the DH badge (its team won a daihyosen).
function rowData(tr, figureSets) {
  const pos = tr.querySelector('td.pool-standings__draw-pos');
  if (!pos) return null;
  const text = (el) => (el ? el.textContent.replace(/\s+/g, ' ').trim() : '');
  const rankText = text(pos);
  const cells = [...tr.querySelectorAll('td.num')].map((td) => text(td));
  const names = figureSets[cells.length] || cells.map((_, i) => `c${i}`);
  const figures = Object.fromEntries(names.map((f, i) => [f, cells[i] === '' ? NaN : Number(cells[i])]));
  return {
    rank: rankText === '' ? NaN : Number(rankText.replace(/\*$/, '')),
    overridden: rankText.endsWith('*'),
    name: text(tr.querySelector('.pool__player-name .numbered-name__text')),
    number: text(tr.querySelector('.pool__player-name .num-prefix')),
    dh: !!tr.querySelector('.pool__player-name .tag-badge'),
    ...figures,
  };
}

// The figure names by how many figure cells a row has: team 8, individual 5,
// engi 2 (wins, flags).
const FIGURE_SETS = { 8: FIGURES, 5: INDIVIDUAL_FIGURES, 2: ['W', 'F'] };

// Every row of the table in its (rank) order.
// The table's empty state ("No matches scored yet.") is not a row.
export async function leagueRows(root) {
  const trs = leagueTable(root).locator('table.pool__table tbody tr');
  const out = [];
  for (let i = 0; i < await trs.count(); i += 1) {
    const row = await trs.nth(i).evaluate(rowData, FIGURE_SETS);
    if (row) out.push(row);
  }
  return out;
}

// One row by name (fails unless exactly one row shows it).
export async function leagueRowFigures(root, name) {
  const row = leagueRow(root, name);
  await expect(row).toHaveCount(1);
  return row.evaluate(rowData, FIGURE_SETS);
}

// name -> rank, for the whole table: what a shared rank reads as.
export async function leagueRanks(root) {
  return Object.fromEntries((await leagueRows(root)).map((r) => [r.name, r.rank]));
}

// The whole block: heading ("Standings" / "Final standings"), matches played
// and in total ("N/M matches"), the Winner badge's name ('' when none), and
// the rows.
export async function readLeagueTable(root) {
  const block = leagueTable(root);
  const count = squash(await block.locator('.pool__match-count').first().innerText());
  const m = /^(\d+)\/(\d+) matches$/.exec(count);
  const badge = root.locator('.winner-badge');
  const winner = (await badge.count()) ? squash(await badge.first().innerText()).replace(/^Winner:\s*/, '') : '';
  return {
    heading: squash(await block.locator('.pool__name').first().innerText()),
    played: m ? Number(m[1]) : NaN,
    total: m ? Number(m[2]) : NaN,
    winner,
    rows: await leagueRows(root),
  };
}

// ------------------------------------------------------------- numbered rows

export const numberedRows = (root) => leagueTable(root).locator('.pool-match-numbered-row');

// The row of the encounter between `shiro` and `aka` (whole names, each on its
// own side), or the row numbered `num`.
export function numberedRow(root, { shiro, aka, num } = {}) {
  const page = pageOf(root);
  let rows = numberedRows(root);
  if (num !== undefined) rows = rows.filter({ has: page.locator('.pool-match-numbered-row__num', { hasText: exact(String(num)) }) });
  if (shiro) rows = rows.filter({ has: page.locator('.pool-match-numbered-row__side--shiro .numbered-name__text', { hasText: exact(shiro) }) });
  if (aka) rows = rows.filter({ has: page.locator('.pool-match-numbered-row__side--aka .numbered-name__text', { hasText: exact(aka) }) });
  return rows;
}

// Every numbered row as { num, shiro, aka, score, running, dhShiro, dhAka },
// in list order. `score` is the centre cell as shown ("vs", "2-1 (3-1)", ...).
export async function readNumberedRows(root) {
  return numberedRows(root).evaluateAll((els) => els.map((el) => {
    const text = (e) => (e ? e.textContent.replace(/\s+/g, ' ').trim() : '');
    const side = (s) => el.querySelector(`.pool-match-numbered-row__side--${s}`);
    return {
      num: Number(text(el.querySelector('.pool-match-numbered-row__num'))),
      shiro: text(side('shiro') && side('shiro').querySelector('.numbered-name__text')),
      aka: text(side('aka') && side('aka').querySelector('.numbered-name__text')),
      score: text(el.querySelector('.pool-match-numbered-row__score')),
      running: el.classList.contains('is-running'),
      dhShiro: !!(side('shiro') && side('shiro').querySelector('.tag-badge')),
      dhAka: !!(side('aka') && side('aka').querySelector('.tag-badge')),
    };
  }));
}

// Tap a numbered row and return the overlay score editor it opened.
export async function openNumberedRow(page, which) {
  const row = numberedRow(page, which);
  await expect(row, `one numbered row for ${JSON.stringify(which)}`).toHaveCount(1);
  await row.tap();
  const ed = page.locator(EDITOR);
  await expect(ed).toBeVisible();
  return ed;
}

// -------------------------------------------------------------------- matrix

export const leagueMatrix = (root) => root.locator('table.league-matrix');

// The matrix cell of `rowName` against `colName`. Its accessible name is
// "Match: <row (dojo)> vs <col (dojo)>: <result>", the dojo only when known.
const matrixCell = (root, rowName, colName) => leagueMatrix(root).getByLabel(new RegExp(
  `^Match: ${escape(rowName)}( \\([^)]*\\))? vs ${escape(colName)}( \\([^)]*\\))?: `,
));

// Tap a matrix cell and return the overlay score editor it opened.
export async function openMatrixCell(page, rowName, colName) {
  await matrixCell(page, rowName, colName).tap();
  const ed = page.locator(EDITOR);
  await expect(ed).toBeVisible();
  return ed;
}

// ----------------------------------------------------------- playing matches

// Play a regular TEAM encounter on the overlay team sheet `ed` and Finish it.
// `bouts` gives each bout in order: 'shiro' or 'aka' (one men for that side),
// 'tie' (hikiwake), or { side, waza: ['M', 'K'] } for several strikes. Every
// bout needs a result before Finish. Starts the match when it is still
// scheduled. Returns the IV/PW band as it read just before Finish.
export async function playEncounter(ed, bouts) {
  const page = ed.page();
  await expect(boutRow(ed, 1)).toBeVisible();
  const start = ed.locator('button').filter({ hasText: /^Start match$/ }).first();
  if (await start.isVisible()) await startMatch(page);
  await running(page, ed);
  for (const [i, b] of bouts.entries()) {
    const n = i + 1;
    const spec = typeof b === 'string' ? { side: b } : b;
    if (spec.side === 'tie') {
      await ensureTie(ed, n);
      continue;
    }
    for (const waza of [].concat(spec.waza || 'M')) await boutIppon(ed, n, spec.side, waza);
  }
  const totals = await teamTotals(ed);
  await finishTeam(ed);
  return totals;
}

// Play a tie-breaker bout (a league's daihyosen or ippon-shobu), which the
// INDIVIDUAL board scores even in a team league, on the overlay editor, and
// Finish it: `winner` 'shiro' | 'aka' takes one `waza` (a league tie-breaker
// needs a winner: the editor refuses a draw, see league-individual F3b).
export async function playTiebreakBout(page, { winner, waza = 'M' } = {}) {
  const ed = page.locator(EDITOR);
  await expect(ed).toBeVisible();
  const start = ed.locator('button').filter({ hasText: /^Start match$/ }).first();
  if (await start.isVisible()) await startMatch(page);
  await running(page, ed);
  await awardIppon(page, winner, waza, EDITOR);
  return finishMatch(page, EDITOR);
}

// The court console's bout between two of `among`, waiting as Up next or
// already running (a Finish + Start Next starts it), as its pairing.
export async function courtBoutAmong(page, among) {
  let pair = null;
  await expect.poll(async () => {
    pair = (await inlineRunning(page))
      ? await editorSides(page).catch(() => null)
      : await sides(upNextCard(page)).catch(() => null);
    return !!pair && among.includes(pair.shiro) && among.includes(pair.aka);
  }, { message: `the court holds a bout between two of ${among.join(', ')}`, timeout: EXPECT_TIMEOUT_MS }).toBe(true);
  return pair;
}

// The court operator plays a tie-breaker bout (a daihyosen or an
// ippon-shobu, on the individual board) on the console already open, and the
// journey's camera takes the four named shots of it: `<flow>-console-up-next`
// (the queue holding it, Up next or already running after the Finish + Start
// Next before it), `-running`, `-before-finish` and `-after`. `winner` names
// the competitor who takes it with one men, or `decide(page, pair)` records
// the result instead (an encho and a hantei). Returns what the console said:
// { pair, upNext (the card's text, '' when it was already running), meta (the editor's meta line),
// finishLabel, shots }.
export async function playTiebreakOnCourt(page, { among, winner, decide, shot, flow }) {
  await courtBoutAmong(page, among);
  const card = upNextCard(page);
  const upNext = (await inlineRunning(page)) ? '' : squash(await card.first().innerText());
  const shots = { upNext: await shot(page, `${flow}-console-up-next`, { fullPage: true }) };
  const pair = await ensureRunning(page);
  expect(pair, 'the tie-breaker bout runs on the court').toBeTruthy();
  expect(among.includes(pair.shiro) && among.includes(pair.aka), `${pair.shiro} v ${pair.aka} is the tie-breaker`).toBe(true);
  const meta = squash((await page.locator(INLINE_EDITOR).first().locator('.editor-modal__meta').allInnerTexts())[0] || '');
  shots.running = await shot(page, `${flow}-console-running`, { fullPage: true });
  if (decide) await decide(page, pair);
  else await awardIppon(page, pair.shiro === winner ? 'shiro' : 'aka', 'M');
  const finishLabel = squash(await finishButton(page).innerText());
  shots.beforeFinish = await shot(page, `${flow}-console-before-finish`, { fullPage: true });
  await finishMatch(page);
  await expect(completedRowFor(page, pair)).toBeVisible();
  shots.after = await shot(page, `${flow}-console-after`, { fullPage: true });
  return { pair, upNext, meta, finishLabel, shots };
}

// ----------------------------------------------------------- confirm dialogs

// The app's confirm (window.confirmDialog, ui.jsx): it carries no accessible
// name unless given a title, so it is found by its shape.
export const confirmDialog = (page) => page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();

// What the open confirm says: { title, message, buttons } (its title, the
// message's sentences as one string, the footer's button labels in order).
export async function readConfirm(page) {
  const dlg = confirmDialog(page);
  await expect(dlg).toBeVisible();
  return {
    title: squash(await dlg.locator('.modal__title').first().innerText()),
    message: squash((await dlg.locator('.dialog-msg').allInnerTexts()).join(' ')),
    buttons: (await dlg.locator('.modal__foot button').allInnerTexts()).map(squash),
  };
}

// Answer the open confirm with the button named `label` (exact), after the
// confirm's own bounce window, and return once that dialog has gone.
export async function answerConfirm(page, label) {
  const dlg = confirmDialog(page);
  await paceSettled(dlg);
  const handle = await dlg.elementHandle();
  try {
    await dlg.locator('.modal__foot').getByRole('button', { name: label, exact: true }).tap();
    await handle.waitForElementState('hidden', { timeout: EXPECT_TIMEOUT_MS });
  } finally {
    await handle.dispose();
  }
}

// ------------------------------------------------------- the tie-break banner

const titled = (page, title) => page.locator('.league-tiebreak').filter({
  has: page.locator('.league-tiebreak__title', { hasText: exact(title) }),
});

export const tiebreakBanner = (page) => titled(page, TEXT.bannerTitle);
export const bannerGroups = (page) => tiebreakBanner(page).locator('.league-tiebreak__group');
const bannerError = (page) => tiebreakBanner(page).locator('.league-tiebreak__err');

// "3rd place" -> { min: 3, max: 3 }; "1st to 2nd place" -> { min: 1, max: 2 }.
const positions = (t) => {
  const m = /(\d+)(?:st|nd|rd|th)?(?:\s*(?:to|[–-])\s*(\d+)(?:st|nd|rd|th)?)?/.exec(t || '');
  return m ? { min: Number(m[1]), max: Number(m[2] || m[1]) } : { min: NaN, max: NaN };
};

// The banner as data: { shown, description, groups, error }. Each group:
// { position (as shown), min, max, teams, run, remove (whether each is
// offered, and enabled), hint }.
export async function readBanner(page) {
  const banner = tiebreakBanner(page);
  if (!(await banner.count())) return { shown: false, groups: [] };
  const groups = await bannerGroups(page).evaluateAll((els) => els.map((g) => {
    const text = (e) => (e ? e.textContent.replace(/\s+/g, ' ').trim() : '');
    const btn = (label) => [...g.querySelectorAll('button')].find((b) => text(b) === label);
    const state = (b) => (b ? { offered: true, enabled: !b.disabled } : { offered: false, enabled: false });
    return {
      position: text(g.querySelector('.league-tiebreak__pos')),
      teams: text(g.querySelector('.league-tiebreak__teams')).split(/, | and /).map((t) => t.trim()).filter(Boolean),
      run: state(btn('Run tie-breaker')),
      remove: state(btn('Remove unscored tie-breaker')),
      hint: [...g.querySelectorAll('.field__hint')].map(text).join(' '),
    };
  }));
  const err = bannerError(page);
  return {
    shown: true,
    description: squash(await banner.locator('.league-tiebreak__desc').first().innerText()),
    groups: groups.map((g) => ({ ...g, ...positions(g.position) })),
    error: (await err.count()) ? squash(await err.first().innerText()) : '',
  };
}

// The banner group of exactly these teams (in any order).
export async function bannerGroup(page, teams) {
  const want = [...teams].sort().join('|');
  const { groups } = await readBanner(page);
  const i = groups.findIndex((g) => [...g.teams].sort().join('|') === want);
  if (i < 0) throw new Error(`bannerGroup: no group of ${teams.join(', ')} (groups: ${JSON.stringify(groups.map((g) => g.teams))})`);
  return bannerGroups(page).nth(i);
}

// The answer to the next league tie-break write: POST (Run) or DELETE (Remove
// unscored tie-breaker). Armed before the tap. Resolves { status, ok, body }.
export function tiebreakWrite(page, method) {
  const re = /^\/api\/competitions\/[^/]+\/league-tiebreak$/;
  const answered = page.waitForResponse((r) => r.request().method() === method && re.test(new URL(r.url()).pathname),
    { timeout: EXPECT_TIMEOUT_MS })
    .then(async (r) => ({ status: r.status(), ok: r.ok(), body: await r.json().catch(() => null) }));
  answered.catch(() => {});
  return answered;
}
const landed = async (answered, what, allowRefusal) => {
  const res = await answered;
  if (!allowRefusal) expect(res.ok, `${what} was answered ${res.status} ${JSON.stringify(res.body)}`).toBe(true);
  return res;
};

// Tap Run tie-breaker on the group of `teams` and return the server's answer
// (a success unless `allowRefusal`), once the group offers Remove instead.
export async function runTiebreaker(page, teams, { allowRefusal = false } = {}) {
  const group = await bannerGroup(page, teams);
  const answered = tiebreakWrite(page, 'POST');
  await group.getByRole('button', { name: TEXT.run, exact: true }).tap();
  const res = await landed(answered, 'Run tie-breaker', allowRefusal);
  if (res.ok) await expect(group.getByRole('button', { name: TEXT.remove, exact: true })).toBeVisible();
  return res;
}

// ------------------------------------------- the drawn-bout notice (built)

const drawnNotice = (page) => page.getByTestId('drawn-tiebreak-notice');

// The notice text(s) on Pools: each notice's title and description, squashed
// (the buttons and the reopen feedback are left out), as TEXT.drawnBout spells.
export async function drawnBoutNotices(page) {
  return drawnNotice(page).evaluateAll((els) => els.map((e) =>
    ['.league-tiebreak__title', '.league-tiebreak__desc']
      .map((s) => e.querySelector(s).textContent).join(' ').replace(/\s+/g, ' ').trim()));
}

// Tap the notice's Open bout (the notice naming `a` and `b` when given) and
// return the overlay editor it opened.
export async function openDrawnBout(page, { a, b } = {}) {
  let notice = drawnNotice(page);
  if (a && b) notice = notice.filter({ hasText: `${a} and ${b}` });
  await notice.last().getByRole('button', { name: TEXT.openBout, exact: true }).tap();
  const ed = page.locator(EDITOR);
  await expect(ed).toBeVisible();
  return ed;
}

// ---------------------------------------------------------- the chusen panel

export const chusenPanel = (page) => titled(page, TEXT.chusenTitle);
const chusenRecordedPanel = (page) => titled(page, TEXT.chusenRecordedTitle);

// The ties waiting for lots: [{ pool, teams, min, max }].
export async function readChusen(page) {
  const panel = chusenPanel(page);
  if (!(await panel.count())) return [];
  return panel.locator('.league-tiebreak__group').evaluateAll((els) => els.map((g) => {
    const text = (e) => (e ? e.textContent.replace(/\s+/g, ' ').trim() : '');
    const range = /places (\d+)(?:st|nd|rd|th) to (\d+)(?:st|nd|rd|th)/.exec(text(g));
    return {
      pool: text(g.querySelector('.league-tiebreak__pos')),
      teams: text(g.querySelector('.league-tiebreak__teams')).split(/, | and /).map((t) => t.trim()).filter(Boolean),
      min: range ? Number(range[1]) : NaN,
      max: range ? Number(range[2]) : NaN,
    };
  }));
}

// How the tie-break notices list the teams in a group: "A", "A and B", "A, B
// and C" (andList, web-mobile/js/list_phrase.jsx). The readers above split it
// back on ", " and " and ", which holds for the one-word names these journeys
// give their teams.
export const andList = (xs) => (xs.length <= 1 ? xs.join('') : `${xs.slice(0, -1).join(', ')} and ${xs[xs.length - 1]}`);

// The individual notice once a tie-breaker round leaves a tie, and its group
// of `names` (in the order the notice lists them).
export const circleNotice = (page) => titled(page, TEXT.circleTitle);
export const circleGroup = (page, names) => circleNotice(page).locator('.league-tiebreak__group').filter({
  has: page.locator('.league-tiebreak__teams', { hasText: exact(andList(names)) }),
});

// The lots entry that holds a number box for each of `names`, wherever it is
// drawn: the team chusen panel, the individual notice (once Draw lots instead
// is tapped, or at once when nobody can fight another round), or a recorded
// group being changed. They all draw one entry: a box per competitor,
// labelled with their name, and Record chusen result.
const lotsEntry = (page, names) => names.reduce(
  (loc, name) => loc.filter({ has: page.getByLabel(name, { exact: true }) }),
  // The innermost group: the individual notice draws the entry inside its own.
  page.locator('.league-tiebreak__group')
    .filter({ hasNot: page.locator('.league-tiebreak__group') })
    .filter({ has: page.getByRole('button', { name: TEXT.recordLots, exact: true }) }),
);

// Enter the order the lots gave: `order` lists the competitors best first.
// Each box gets its place (the entry's first position, then one more each),
// Record chusen result is tapped, and the call returns the server's answer to
// PUT .../pools/<pool>/override-rank ({ status, ok, body }) once it succeeded.
export async function recordLots(page, order) {
  const entry = lotsEntry(page, order);
  await expect(entry, `one lots entry for ${order.join(', ')}`).toHaveCount(1);
  const from = /places (\d+)(?:st|nd|rd|th) to (\d+)(?:st|nd|rd|th)/i.exec(squash(await entry.innerText()));
  expect(from, 'the lots entry names the places it assigns').toBeTruthy();
  const first = Number(from[1]);
  for (const [i, name] of order.entries()) await entry.getByLabel(name, { exact: true }).fill(String(first + i));
  const answered = page.waitForResponse((r) => r.request().method() === 'PUT'
    && /^\/api\/competitions\/[^/]+\/pools\/[^/]+\/override-rank$/.test(new URL(r.url()).pathname), { timeout: EXPECT_TIMEOUT_MS })
    .then(async (r) => ({ status: r.status(), ok: r.ok(), body: await r.json().catch(() => null) }));
  answered.catch(() => {});
  await entry.getByRole('button', { name: TEXT.recordLots, exact: true }).tap();
  const res = await answered;
  expect(res.ok, `Record chusen result was answered ${res.status} ${JSON.stringify(res.body)}`).toBe(true);
  return res;
}

// The recorded lots: [{ pool, order }] with `order` as shown ("1st A, 2nd B").
export async function readChusenRecorded(page) {
  const panel = chusenRecordedPanel(page);
  if (!(await panel.count())) return [];
  return panel.locator('.league-tiebreak__group').evaluateAll((els) => els.map((g) => {
    const text = (e) => (e ? e.textContent.replace(/\s+/g, ' ').trim() : '');
    return {
      pool: text(g.querySelector('.league-tiebreak__pos')),
      order: text(g.querySelector('.league-tiebreak__teams')).replace(/^Drawing lots:\s*/, ''),
    };
  }));
}

// ------------------------------------------------------------ public readers

// The Overview's league Winner badge.
export const overviewWinner = (page) => page.getByTestId('league-overview-winner');

// Whether the public page offers the Awards tab (only once completed).
export const awardsTabOffered = async (page) => (await competitionTab(page, 'Awards').count()) > 0;

// The public Awards tab of a completed competition.
export async function openViewerAwards(page, id) {
  await openViewer(page, id);
  await competitionTab(page, 'Awards').tap();
  await expect(page.getByTestId('awards-view').or(page.getByTestId('awards-empty'))).toBeVisible();
}

// The page does not scroll sideways at any of `widths` (a small phone, a phone,
// a large phone) and no Awards place card is cut off at either edge; the
// viewport goes back to its size afterwards. `atWidth(width)` runs the caller's
// own checks at each width.
export async function expectNoSidewaysScroll(page, widths = [320, 375, 414], atWidth = null) {
  const original = page.viewportSize();
  try {
    for (const width of widths) {
      await page.setViewportSize({ width, height: original ? original.height : 844 });
      const seen = await page.evaluate(() => ({
        page: document.documentElement.scrollWidth - window.innerWidth,
        clipped: [...document.querySelectorAll('[data-testid^="awards-place-"]')].filter((el) => {
          const r = el.getBoundingClientRect();
          return r.left < -0.5 || r.right > window.innerWidth + 0.5;
        }).length,
      }));
      expect(seen.page, `the page scrolls sideways at ${width}px`).toBeLessThanOrEqual(0);
      expect(seen.clipped, `an Awards place is cut off at ${width}px`).toBe(0);
      if (atWidth) await atWidth(width);
    }
  } finally {
    if (original) await page.setViewportSize(original);
  }
}

// How many lines a control's label takes (1 when it does not wrap): the
// distinct tops of the line boxes of its text nodes only, so an inline child
// such as a busy spinner adds no line.
export const labelLines = (locator) => locator.evaluate((el) => {
  const tops = new Set();
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const range = document.createRange();
    range.selectNodeContents(node);
    for (const r of range.getClientRects()) if (r.width > 0) tops.add(Math.round(r.top));
  }
  return tops.size;
});

// The podium as [{ place, label, name, dojo }] in the order shown, plus the
// league Winner badge (`winner`, '' when none). Empty when the tab says the
// final standings are not yet available.
export async function readPodium(page) {
  const view = page.getByTestId('awards-view');
  if (!(await view.count())) return { winner: '', places: [] };
  const places = await view.locator('[data-testid^="awards-place-"]').evaluateAll((els) => els.map((el) => {
    const text = (e) => (e ? e.textContent.replace(/\s+/g, ' ').trim() : '');
    return {
      place: Number(/^awards-place-(\d+)-/.exec(el.getAttribute('data-testid'))[1]),
      label: text(el.querySelector('.place, .awards-hero__place')),
      name: text(el.querySelector('.name, .awards-hero__name')),
      dojo: text(el.querySelector('.dojo, .awards-hero__dojo')),
    };
  }));
  const badge = page.getByTestId('league-winner-badge');
  const winner = (await badge.count()) ? squash(await badge.innerText()).replace(/^Winner:\s*/, '') : '';
  return { winner, places };
}

// ------------------------------------------------------------ a backup copy
//
// A league saved by an earlier release (a setting the app no longer offers, a
// record no screen can make any more) reaches the app the way such a
// tournament does: a backup archive, restored through Backup and restore. The
// archive is the app's own backup of a league, with files rewritten; the
// manifest's size and SHA-256 of each rewritten file are rewritten with it
// (the restore checks both).

// Back the competition named `name` up from Backup and restore; returns the
// archive's path inside `dir`.
export async function backUpCompetition(page, name, dir) {
  await openBackupPage(page);
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('button', { name: `Back up "${name}"`, exact: true }).tap(),
  ]);
  const file = path.join(dir, download.suggestedFilename());
  await download.saveAs(file);
  return file;
}

// Write to `out` the archive `file` with the entries rewritten: `edits` maps
// the end of an entry's name ('/config.md', '/pool-matches.csv') to a function
// of its text that returns the new text. Each edit must match exactly one
// entry. Returns the names rewritten.
export function rewriteBackup(file, out, edits) {
  const entries = readZip(fs.readFileSync(file));
  const manifest = JSON.parse(entries.get('manifest.json').toString('utf8'));
  const rewritten = [];
  for (const [tail, edit] of Object.entries(edits)) {
    const names = [...entries.keys()].filter((n) => n.endsWith(tail));
    if (names.length !== 1) throw new Error(`rewriteBackup: ${names.length} entries end with ${tail}: ${[...entries.keys()].join(', ')}`);
    const [name] = names;
    const body = Buffer.from(edit(entries.get(name).toString('utf8')), 'utf8');
    entries.set(name, body);
    const listed = manifest.files.find((f) => f.path === name);
    listed.size = body.length;
    listed.sha256 = crypto.createHash('sha256').update(body).digest('hex');
    rewritten.push(name);
  }
  entries.set('manifest.json', Buffer.from(JSON.stringify(manifest, null, 2), 'utf8'));
  fs.writeFileSync(out, writeZip(entries));
  return rewritten;
}

// config.md with `line` added to its front matter (after the opening "---").
export function withFrontMatterLine(text, line) {
  if (!text.startsWith('---\n')) throw new Error(`config.md has no front matter: ${text.slice(0, 40)}`);
  return `---\n${line}\n${text.slice(4)}`;
}

// Restore the competition named `name` from `archive` as a copy named
// `copyName` (Backup and restore, confirmed with the tournament password);
// returns the copy's id.
export async function restoreAsCopy(page, { archive, name, copyName, password }) {
  await openBackupPage(page);
  await chooseBackup(page, archive);
  const row = planRow(page, name);
  await row.getByRole('radio', { name: /Restore as a copy named/ }).check();
  await row.getByRole('textbox', { name: `Name for the copy of "${name}"` }).fill(copyName);
  await expect(restoreButton(page)).toHaveText('Restore');
  const answered = page.waitForResponse((r) => r.request().method() === 'POST'
    && new URL(r.url()).pathname === '/api/tournament/restore');
  await restoreButton(page).tap();
  await confirmWithPassword(page, 'Confirm restore', 'Restore', password);
  const res = await answered;
  expect(res.ok(), `restore answered ${res.status()}`).toBe(true);
  const body = await res.json();
  await expect(page.getByRole('heading', { level: 2, name: 'Restore completed' })).toBeVisible();
  const copyId = ((body.plan && body.plan.competitions) || []).find((c) => c.newId)?.newId;
  expect(copyId, 'the copy has an id').toBeTruthy();
  return copyId;
}
