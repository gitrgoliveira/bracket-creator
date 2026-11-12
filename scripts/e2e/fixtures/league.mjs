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
//                         one group per consequential tie, Run tie-breaker or
//                         Remove unscored tie-breaker, Accept shared ranks and
//                         its confirm; the Undo control the fix adds
//   the drawn-bout notice the line the fix adds above the table for a
//                         tie-breaker saved as a draw, with Open bout
//   the chusen panel      "Chusen (drawing lots) required" / "... recorded"
//   the settings          "Break ties for top" pills (team league only) and the
//                         "Award two joint 3rd places" checkbox on
//                         /admin/competition/:id/settings; both lock once a draw
//                         exists, so they are set before Generate draw
//   the public page       the League tab, the Overview's league summary, the
//                         Awards tab and its podium
//
// The controls the league review's fix adds (the Undo control and its confirm,
// the new Accept confirm, the finished-league warning, the drawn-bout notice)
// are found by role and the visible text the plan fixes word for word (TEXT
// below); the fix builds them to match, so a journey written against today's
// app only has to drop its test.fixme once they exist. Readers and confirm
// helpers READ what is on screen and return it; the journeys assert.
import { expect } from '@playwright/test';
import { EXPECT_TIMEOUT_MS } from './devices.mjs';
import { settled as paceSettled } from './pace.mjs';
import { FIGURES, INDIVIDUAL_FIGURES } from './pools.mjs';
import { competitionTab, openViewer } from './public.mjs';
import { EDITOR, awardIppon, finishMatch, markDraw, running, startMatch } from './scoring.mjs';
import { boutIppon, boutRow, ensureTie, finishTeam, teamTotals } from './team.mjs';
import { createCompetition } from './wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from './competition.mjs';

// The words on screen. Today's labels where the control exists; the plan's
// fixed wording for what the fix adds (marked "fix").
export const TEXT = {
  bannerTitle: 'Tie-breaker required',
  chusenTitle: 'Chusen (drawing lots) required',
  chusenRecordedTitle: 'Chusen (drawing lots) recorded',
  run: 'Run tie-breaker',
  remove: 'Remove unscored tie-breaker',
  removeConfirm: 'Remove',
  // The banner's button reads "Accept shared ranks / no tie-breaker" today.
  accept: /^Accept shared ranks/,
  acceptConfirm: 'Accept shared ranks',
  cancel: 'Cancel',
  recordChusen: 'Record chusen result',
  // fix: the Undo control, its line and its confirm.
  undo: 'Undo shared ranks',
  undoConfirm: 'Undo shared ranks',
  acceptedLine: /^Shared ranks accepted:/,
  undoQuestion: 'Undo the shared ranks? The league goes back to match play and the tie is asked again: run a tie-breaker or accept shared ranks.',
  undoDeadCourt: (court) => `Matches on shiaijo ${court}, which the tournament no longer has, become unassigned.`,
  // fix: the Accept confirm. `place` is written as the app writes it.
  acceptQuestion: (teams, place) => `Accept shared ranks? ${teams.join(', ')} share ${place}. The competition finishes now. Undo shared ranks brings the question back.`,
  acceptRemovesBout: (a, b) => `This also removes the tie-breaker not yet fought: ${a} v ${b}.`,
  acceptCycleLots: 'Drawing lots is the other way to separate them.',
  // fix: finalize refused while a regular match is unfinished.
  notSettled: 'A league match is not finished yet, so the table can still change. Finish it, then decide.',
  // fix: a result changed on a finished league.
  finishedLeague: 'This league has finished. Changing this result can change the final table and podium. If it leaves a new tie at a podium place, the league goes back to match play to settle it.',
  changeAnyway: 'Change it anyway',
  // fix: a tie-breaker needs a winner (the write's refusal, and the notice).
  tiebreakNeedsWinner: 'A tie-breaker needs a winner: continue with encho, then decide by hantei.',
  drawnBout: (a, b) => `${a} v ${b} was saved as a draw. A tie-breaker needs a winner: open it and decide by encho or hantei.`,
  openBout: 'Open bout',
  // settings
  tiebreakTop: 'Break ties for top',
  twoThirdPlaces: 'Award two joint 3rd places',
};

export const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const exact = (s) => new RegExp(`^${escape(s)}$`);
// A `has:` locator must be built from the page, never chained off the root.
const pageOf = (root) => (typeof root.page === 'function' ? root.page() : root);
export const squash = (t) => (t || '').replace(/\s+/g, ' ').trim();

// ------------------------------------------------------------------- seeding

// Create a league through the wizard, set its league settings, paste the
// roster, draw and start it, and return its id. `entrants` are [name, dojo]
// pairs (teams for kind 'team'). `topN` (3 | 4) is the "Break ties for top"
// pill, which only the settings page offers; `twoThirdPlaces` is the wizard's
// checkbox. Both are set before the draw, which locks them. `draw: false`
// stops after the roster, `start: false` after the draw.
export async function seedLeague(page, {
  name, kind = 'team', entrants, teamSize = 3, teamMatchType = 'fixed', courts = ['A'], numberPrefix,
  twoThirdPlaces, topN, draw = true, start = true,
} = {}) {
  const team = kind === 'team';
  const id = await createCompetition(page, {
    name, kind, format: 'league', courts, numberPrefix, twoThirdPlaces,
    ...(team ? { teamSize, teamMatchType } : {}),
  });
  if (topN !== undefined) {
    await openLeagueSettings(page, id);
    await chooseTiebreakTop(page, topN);
    await saveSettings(page, id);
  }
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

// The "Break ties for top" row (a PillGroup: role="group" named by its label,
// each pill a button whose aria-pressed says whether it is the stored value).
export const tiebreakTopGroup = (page) => page.getByRole('group', { name: new RegExp(`^${escape(TEXT.tiebreakTop)}`) });
export const twoThirdPlacesBox = (page) => page.getByRole('checkbox', { name: TEXT.twoThirdPlaces });

// Everything the two league settings say: whether each is shown, what is
// chosen, whether it is locked, and the hint beside it (the settings copy F9
// reads).
export async function readLeagueSettings(page) {
  const grp = tiebreakTopGroup(page);
  const box = twoThirdPlacesBox(page);
  const out = { tiebreakTop: { shown: false }, twoThirdPlaces: { shown: false } };
  if (await grp.count()) {
    const pills = grp.getByRole('button');
    const options = [];
    for (let i = 0; i < await pills.count(); i += 1) {
      const p = pills.nth(i);
      options.push({
        label: squash(await p.innerText()),
        pressed: (await p.getAttribute('aria-pressed')) === 'true',
        disabled: await p.isDisabled(),
      });
    }
    const hint = grp.locator('xpath=ancestor::div[contains(concat(" ", normalize-space(@class), " "), " field ")][1]').locator('.field__hint');
    out.tiebreakTop = {
      shown: true,
      options,
      active: (options.find((o) => o.pressed) || {}).label || null,
      locked: options.length > 0 && options.every((o) => o.disabled),
      hint: (await hint.count()) ? squash(await hint.first().innerText()) : '',
    };
  }
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

// Choose "Top 3" or "Top 4" (not saved until saveSettings).
export async function chooseTiebreakTop(page, n) {
  const pill = tiebreakTopGroup(page).getByRole('button', { name: `Top ${n}`, exact: true });
  await pill.tap();
  await expect(pill).toHaveAttribute('aria-pressed', 'true');
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
    rank: Number(rankText.replace(/\*$/, '')),
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
export const matrixCell = (root, rowName, colName) => leagueMatrix(root).getByLabel(new RegExp(
  `^Match: ${escape(rowName)}( \\([^)]*\\))? vs ${escape(colName)}( \\([^)]*\\))?: `,
));

// What the cell says of the row competitor: "Win", "Loss", "Draw", "Pending",
// "Now" or "Result not attributed".
export async function matrixResult(root, rowName, colName) {
  const label = await matrixCell(root, rowName, colName).getAttribute('aria-label');
  return label.slice(label.lastIndexOf(': ') + 2);
}

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
// Finish it: `winner` 'shiro' | 'aka' takes one `waza`, or `draw: true` marks
// it drawn (which the fix refuses: the caller then reads the editor instead).
export async function playTiebreakBout(page, { winner, waza = 'M', draw = false } = {}) {
  const ed = page.locator(EDITOR);
  await expect(ed).toBeVisible();
  const start = ed.locator('button').filter({ hasText: /^Start match$/ }).first();
  if (await start.isVisible()) await startMatch(page);
  await running(page, ed);
  if (draw) await markDraw(page, EDITOR);
  else await awardIppon(page, winner, waza, EDITOR);
  return finishMatch(page, EDITOR);
}

// ----------------------------------------------------------- confirm dialogs

// The app's confirm (window.confirmDialog, ui.jsx): it carries no accessible
// name unless given a title, so it is found by its shape.
export const confirmDialog = (page) => page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();

// What the open confirm says: { message, buttons } (the message's sentences
// as one string, the footer's button labels in order).
export async function readConfirm(page) {
  const dlg = confirmDialog(page);
  await expect(dlg).toBeVisible();
  return {
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
export const bannerError = (page) => tiebreakBanner(page).locator('.league-tiebreak__err');
export const acceptButton = (page) => tiebreakBanner(page).getByRole('button', { name: TEXT.accept });

// "Position 3" -> { min: 3, max: 3 }; "Positions 1–2" -> { min: 1, max: 2 }.
const positions = (t) => {
  const m = /(\d+)(?:\s*[–-]\s*(\d+))?/.exec(t || '');
  return m ? { min: Number(m[1]), max: Number(m[2] || m[1]) } : { min: NaN, max: NaN };
};

// The banner as data: { shown, description, groups, acceptOffered, error }.
// Each group: { position (as shown), min, max, teams, run, remove (whether
// each is offered, and enabled), hint }.
export async function readBanner(page) {
  const banner = tiebreakBanner(page);
  if (!(await banner.count())) return { shown: false, groups: [] };
  const groups = await bannerGroups(page).evaluateAll((els) => els.map((g) => {
    const text = (e) => (e ? e.textContent.replace(/\s+/g, ' ').trim() : '');
    const btn = (label) => [...g.querySelectorAll('button')].find((b) => text(b) === label);
    const state = (b) => (b ? { offered: true, enabled: !b.disabled } : { offered: false, enabled: false });
    return {
      position: text(g.querySelector('.league-tiebreak__pos')),
      teams: text(g.querySelector('.league-tiebreak__teams')).split(' · ').map((t) => t.trim()).filter(Boolean),
      run: state(btn('Run tie-breaker')),
      remove: state(btn('Remove unscored tie-breaker')),
      hint: [...g.querySelectorAll('.field__hint')].map(text).join(' '),
    };
  }));
  const err = bannerError(page);
  const accept = acceptButton(page);
  return {
    shown: true,
    description: squash(await banner.locator('.league-tiebreak__desc').first().innerText()),
    groups: groups.map((g) => ({ ...g, ...positions(g.position) })),
    acceptOffered: (await accept.count()) > 0,
    acceptLabel: (await accept.count()) ? squash(await accept.first().innerText()) : '',
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

// The answer to the next league tie-break write: `method` POST (Run), DELETE
// (Remove), or with `tail` '/finalize' POST (Accept) and DELETE (Undo, the
// fix). Armed before the tap. Resolves { status, ok, body }.
function tiebreakWrite(page, method, tail = '') {
  const re = new RegExp(`^/api/competitions/[^/]+/league-tiebreak${escape(tail)}$`);
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

// Tap Remove unscored tie-breaker on the group of `teams`, leaving its confirm
// open; returns what the confirm says.
export async function requestRemoveTiebreaker(page, teams) {
  const group = await bannerGroup(page, teams);
  await group.getByRole('button', { name: TEXT.remove, exact: true }).tap();
  return readConfirm(page);
}

// Remove the unscored tie-breaker of `teams`: tap, answer the confirm with
// Remove, return the server's answer.
export async function removeTiebreaker(page, teams, { allowRefusal = false } = {}) {
  const asked = await requestRemoveTiebreaker(page, teams);
  const answered = tiebreakWrite(page, 'DELETE');
  await answerConfirm(page, TEXT.removeConfirm);
  return { asked, ...(await landed(answered, 'Remove unscored tie-breaker', allowRefusal)) };
}

// Tap Accept shared ranks, leaving its confirm open; returns what it says.
export async function requestAcceptSharedRanks(page) {
  await acceptButton(page).first().tap();
  return readConfirm(page);
}

// Accept the shared ranks: tap, answer the confirm with "Accept shared ranks",
// return { asked, status, ok, body }. A refusal (the fix's league_not_settled)
// is returned, not thrown, when `allowRefusal`.
export async function acceptSharedRanks(page, { allowRefusal = false } = {}) {
  const asked = await requestAcceptSharedRanks(page);
  const answered = tiebreakWrite(page, 'POST', '/finalize');
  await answerConfirm(page, TEXT.acceptConfirm);
  return { asked, ...(await landed(answered, 'Accept shared ranks', allowRefusal)) };
}

// --------------------------------------------- the Undo control (the fix)

// The page's own "Undo shared ranks" button, never the one in its confirm.
export const undoControl = (page) => page.locator(
  `xpath=//button[normalize-space(.)="${TEXT.undo}"][not(ancestor::*[@role="dialog"])]`,
);

// The "Shared ranks accepted: <place>: <team>, <team>." lines, one per group.
export async function acceptedLines(page) {
  return (await page.getByText(TEXT.acceptedLine).allInnerTexts()).map(squash);
}

export async function requestUndoSharedRanks(page) {
  await undoControl(page).first().tap();
  return readConfirm(page);
}

// Undo the accepted shared ranks: tap, answer "Undo shared ranks", return the
// server's answer to DELETE .../league-tiebreak/finalize.
export async function undoSharedRanks(page, { allowRefusal = false } = {}) {
  const asked = await requestUndoSharedRanks(page);
  const answered = tiebreakWrite(page, 'DELETE', '/finalize');
  await answerConfirm(page, TEXT.undoConfirm);
  return { asked, ...(await landed(answered, 'Undo shared ranks', allowRefusal)) };
}

// ------------------------------------------- the drawn-bout notice (the fix)

const DRAWN_TAIL = / was saved as a draw\. A tie-breaker needs a winner: open it and decide by encho or hantei\.$/;

// The notice text(s), each "<side> v <side> was saved as a draw. ...".
export async function drawnBoutNotices(page) {
  return (await page.getByText(DRAWN_TAIL).allInnerTexts()).map(squash);
}

// Tap the notice's Open bout (the notice naming `a` v `b` when given) and
// return the overlay editor it opened.
export async function openDrawnBout(page, { a, b } = {}) {
  let notice = page.locator('div, p, section').filter({ hasText: DRAWN_TAIL })
    .filter({ has: page.getByRole('button', { name: TEXT.openBout, exact: true }) });
  if (a && b) notice = notice.filter({ hasText: `${a} v ${b}` });
  await notice.last().getByRole('button', { name: TEXT.openBout, exact: true }).tap();
  const ed = page.locator(EDITOR);
  await expect(ed).toBeVisible();
  return ed;
}

// ---------------------------------------------------------- the chusen panel

export const chusenPanel = (page) => titled(page, TEXT.chusenTitle);
export const chusenRecordedPanel = (page) => titled(page, TEXT.chusenRecordedTitle);

// The ties waiting for lots: [{ pool, teams, min, max }].
export async function readChusen(page) {
  const panel = chusenPanel(page);
  if (!(await panel.count())) return [];
  return panel.locator('.league-tiebreak__group').evaluateAll((els) => els.map((g) => {
    const text = (e) => (e ? e.textContent.replace(/\s+/g, ' ').trim() : '');
    const range = /positions (\d+) to (\d+)/.exec(text(g));
    return {
      pool: text(g.querySelector('.league-tiebreak__pos')),
      teams: text(g.querySelector('.league-tiebreak__teams')).split(' · ').map((t) => t.trim()).filter(Boolean),
      min: range ? Number(range[1]) : NaN,
      max: range ? Number(range[2]) : NaN,
    };
  }));
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

// Record the lots for the tie holding `order`'s teams: `order` lists the teams
// in the finishing order the lots gave. Types each team's position, taps
// Record chusen result, and returns the server's answer to the override-rank
// PUT ({ status, ok, body }).
export async function recordChusen(page, order) {
  const want = [...order].sort().join('|');
  const groups = await readChusen(page);
  const i = groups.findIndex((g) => [...g.teams].sort().join('|') === want);
  if (i < 0) throw new Error(`recordChusen: no chusen tie of ${order.join(', ')} (ties: ${JSON.stringify(groups.map((g) => g.teams))})`);
  const group = chusenPanel(page).locator('.league-tiebreak__group').nth(i);
  for (const [k, team] of order.entries()) {
    const box = group.getByLabel(team, { exact: true });
    await box.fill(String(groups[i].min + k));
    await expect(box).toHaveValue(String(groups[i].min + k));
  }
  const answered = page.waitForResponse((r) => r.request().method() === 'PUT'
    && /\/pools\/[^/]+\/override-rank$/.test(new URL(r.url()).pathname), { timeout: EXPECT_TIMEOUT_MS })
    .then(async (r) => ({ status: r.status(), ok: r.ok(), body: await r.json().catch(() => null) }));
  answered.catch(() => {});
  await group.getByRole('button', { name: TEXT.recordChusen, exact: true }).tap();
  return answered;
}

// ------------------------------------------------------------ public readers

// The Overview's league summary (top five) and its Winner badge.
export const overviewStandings = (page) => page.getByTestId('league-overview-standings');
export const overviewWinner = (page) => page.getByTestId('league-overview-winner');

// Whether the public page offers the Awards tab (only once completed).
export const awardsTabOffered = async (page) => (await competitionTab(page, 'Awards').count()) > 0;

// The public Awards tab of a completed competition.
export async function openViewerAwards(page, id) {
  await openViewer(page, id);
  await competitionTab(page, 'Awards').tap();
  await expect(page.getByTestId('awards-view').or(page.getByTestId('awards-empty'))).toBeVisible();
}

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
      label: text(el.querySelector('.place, .awards-hero__eyebrow')),
      name: text(el.querySelector('.name, .awards-hero__name')),
      dojo: text(el.querySelector('.dojo, .awards-hero__dojo')),
    };
  }));
  const badge = page.getByTestId('league-winner-badge');
  const winner = (await badge.count()) ? squash(await badge.innerText()).replace(/^Winner:\s*/, '') : '';
  return { winner, places };
}
