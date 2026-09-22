// Result captures: surfaces that show a RECORDED outcome.
//
// These four shots carry the group's fixture risk, because every one of them
// displays a value that only exists when the application itself produced it:
// the competitor-number chips on a team's members, the ippon letters in a
// pool's head-to-head grid, the IV/PW tie-break columns of a team Swiss table,
// the flag totals of an engi one. Writing a lineup over HTTP with names alone
// never attaches those names to the numbered blank members, so the chips come
// out empty; quick-scoring a team encounter records no bout points, so PW/PL
// come out zero. Both have shipped a misleading screenshot before.
//
// So the split is: the API creates the tournament, the competitions, their
// participants and their draws (lib/api.mjs states that boundary), and every
// lineup and every score below is entered by DRIVING THE REAL INTERFACE from
// the family seed - the Lineups page, the individual editor, the fixed-order
// team sheet, the engi flag counter. The recipes' assert() then re-reads the
// files on disk and refuses to capture a fixture that lost what the shot
// exists to show.
//
// One tournament holds all four competitions: the mobile app is single-
// tournament, so a family cannot have one of its own.
import fs from 'node:fs';
import path from 'node:path';
import { loginAdmin, settle } from '../lib/ui.mjs';
import { assertLineupIds, csvRows } from '../lib/fixture.mjs';

const TEAM_COMP = 'team-championship';
const SWISS_TEAM = 'swiss-teams';
const SWISS_ENGI = 'swiss-engi';
const POOLS_COMP = 'women-up-to-2d';

// The five FIK fighting-order positions, in sheet order. The Lineups page
// renders one <select data-testid="lineup-position-<key>"> per position
// (admin_lineup.jsx:807-809).
const POSITIONS = ['senpo', 'jiho', 'chuken', 'fukusho', 'taisho'];

const LINEUP_NAMES = [
  'Haruki Tanaka', 'Ren Suzuki', 'Sota Yamamoto', 'Yuto Watanabe', 'Riku Nakamura',
];

const TEAMS = [
  { name: 'Kenshinkan A', dojo: 'Kenshinkan' },
  { name: 'Kenshinkan B', dojo: 'Kenshinkan' },
  { name: 'Mumeishi', dojo: 'Mumeishi' },
  { name: 'Nenriki', dojo: 'Nenriki' },
];

const SWISS_TEAMS = [
  { name: 'Team Aoi', dojo: 'Aoi Dojo' },
  { name: 'Team Bara', dojo: 'Bara Dojo' },
  { name: 'Team Chiba', dojo: 'Chiba Dojo' },
  { name: 'Team Daito', dojo: 'Daito Dojo' },
];

// An engi competitor is a PAIR but a SINGLE participant: both member names go
// in Name, joined by " - " (CLAUDE.md, participant CSV schema). The viewer
// splits them again for display via window.engiPairParts.
const ENGI_PAIRS = [
  { name: 'Alpha One - Alpha Two', dojo: 'Aoi' },
  { name: 'Bravo One - Bravo Two', dojo: 'Bara' },
  { name: 'Charlie One - Charlie Two', dojo: 'Chiba' },
  { name: 'Delta One - Delta Two', dojo: 'Daito' },
];

const POOL_DOJOS = [
  'Team Alpha', 'Team Beta', 'Team Delta', 'Team Epsilon',
  'Team Mu', 'Team Psi', 'Team Rho', 'Team Tau', 'Team Theta', 'Team Xi', 'Team Zeta',
];

// 19 competitors is what the committed capture shows: at poolSize 3 that is
// six pools (one of four), which is what makes the Pools tab a two-column
// grid three rows deep.
const POOL_NAMES = [
  'Vincent King', 'Charles Dickens', 'Arthur Conan', 'Herman Melville',
  'Xavier Lopez', 'George Orwell', 'Mary Shelley', 'Daniel Defoe',
  'Isaac Asimov', 'Kurt Vonnegut', 'William Wright', 'Fyodor Dostoevsky',
  'Nathaniel Hawthorne', 'Yosef Hill', 'Emily Bronte', 'Lewis Carroll',
  'Bram Stoker', 'Jane Austen', 'Oscar Wilde',
];

// ---------------------------------------------------------------------------
// Local UI helpers.
//
// These are deliberately NOT in lib/ui.mjs: they are the first driver for the
// fixed-order team sheet, the engi flag counter and the score-list loop, and
// lib/ui.mjs is edited concurrently. They are candidates to move there once
// the harness settles (noted in the group's report).
// ---------------------------------------------------------------------------

const EDITOR = '[data-testid="scoring-modal-root"], .editor-modal';

// An ippon button is one CHARACTER (M/K/D/T/H, admin_scoring_shared.jsx:35-37),
// so match it exactly: has-text("M") would also hit a "MK" label elsewhere.
function ipponButton(scope, waza) {
  return scope.locator('button.ipt-btn').filter({ hasText: new RegExp(`^${waza}$`) }).first();
}

async function openEditorForRow(page, row) {
  await row.locator('button').filter({ hasText: /^Score$/ }).first().click();
  await page.locator(EDITOR).first().waitFor({ state: 'visible', timeout: 15000 });
  const start = page.locator(EDITOR).locator('button').filter({ hasText: /^Start match$/ }).first();
  if (await start.count()) {
    await start.click();
    await settle(page, 300);
  }
}

// Finishing is a two-tap guard: the button arms ("Tap again to finish"), only
// the second tap submits (admin_scoring_individual.jsx:1176/1184, and the same
// pair on the team sheet at admin_scoring_team.jsx:3850/3859).
//
// The two labels are alternatives, not a preference: the editor offers
// "Finish + Start Next →" whenever another match waits on the same shiaijo and
// the bare "Finish" only when none does. So the chained form is the normal one,
// and it leaves the editor OPEN on the next match - which would sit over the
// list this loop clicks. Dismiss it; the loop re-reads the list and reopens.
async function finishMatch(page) {
  const modal = page.locator(EDITOR).first();
  const plain = modal.locator('button').filter({ hasText: /^Finish$/ }).first();
  const chained = modal.locator('button').filter({ hasText: /^Finish \+ Start Next/ }).first();
  const btn = (await plain.count()) ? plain : chained;
  await btn.click();
  const armed = modal.locator('button').filter({ hasText: /^Tap again to finish/ }).first();
  if (await armed.count()) await armed.click();
  await settle(page, 600);
  await closeEditor(page);
}

// Dismiss whatever editor is open, if one still is.
async function closeEditor(page) {
  const modal = page.locator(EDITOR).first();
  for (let attempt = 0; attempt < 3; attempt += 1) {
    if (!(await modal.count())) return;
    const close = modal.locator('button').filter({ hasText: /^(✕ Close|Close|Cancel)$/ }).first();
    if (await close.count()) await close.click().catch(() => {});
    else await page.keyboard.press('Escape');
    await modal.waitFor({ state: 'detached', timeout: 4000 }).catch(() => {});
  }
}

// The admin competition's Scores list. Every unscored match carries a "Score"
// button; a completed one says "Correct" (admin_schedule_score_editor.jsx:243).
function scorableRows(page) {
  return page.locator('.score-edit-row').filter({ has: page.locator('button', { hasText: /^Score$/ }) });
}

async function gotoScores(page, base, compId) {
  await page.goto(`${base}/admin/competition/${compId}/scores`, { waitUntil: 'domcontentloaded' });
  await page.locator('.score-edit-row').first().waitFor({ state: 'visible', timeout: 8000 }).catch(() => {});
  await settle(page, 400);
}

// Score every scorable match in a competition by driving the editor, calling
// `score(editor, page, index)` once per match to enter that match's result.
// Re-reads the list after each save: finishing a pool can seed fresh knockout
// matches into the same list.
async function scoreEveryMatch(page, base, compId, score, limit = 60) {
  let done = 0;
  await gotoScores(page, base, compId);
  for (let guard = 0; guard < limit; guard += 1) {
    const rows = scorableRows(page);
    if (!(await rows.count())) {
      // The list is live over SSE, so it empties in place as matches finish.
      // Reload once before giving up: a pool completing seeds its qualifiers
      // into knockout matches that were not in this list a moment ago.
      await gotoScores(page, base, compId);
      if (!(await scorableRows(page).count())) break;
    }
    await openEditorForRow(page, scorableRows(page).first());
    await score(page.locator(EDITOR).first(), page, done);
    await finishMatch(page);
    done += 1;
  }
  return done;
}

// An individual bout: the winner takes two ippons, the loser sometimes one, so
// the head-to-head grid shows letters on both sides of the diagonal and PW/PL
// are not all the same number.
// The loser's point goes in FIRST: a side reaching two ippons decides the bout
// and the editor then disables the other side's buttons (`boutDecided`,
// admin_scoring_individual.jsx:843).
async function scoreIndividualBout(editor, page, i) {
  const winner = i % 2 === 0 ? 'aka' : 'shiro';
  const loser = winner === 'aka' ? 'shiro' : 'aka';
  if (i % 3 === 2) {
    await ipponButton(editor.locator(`.sb-side--${loser}`), 'K').click();
    await settle(page, 100);
  }
  const win = editor.locator(`.sb-side--${winner}`);
  await ipponButton(win, i % 3 === 0 ? 'M' : 'K').click();
  await settle(page, 100);
  await ipponButton(win, i % 3 === 1 ? 'D' : 'M').click();
  await settle(page, 100);
}

// Per-encounter bout outcomes. They vary on purpose: the point of the team
// Swiss capture is the TIE-BREAK columns, and an encounter profile that gives
// every team one win, one loss and one draw fills IV / IL / IT with the same
// number on every row, which demonstrates nothing.
const TEAM_PROFILES = [
  ['aka', 'aka', 'shiro'],
  ['shiro', 'draw', 'shiro'],
  ['aka', 'draw', 'draw'],
  ['shiro', 'aka', 'aka'],
];

// A fixed-order team encounter: every numbered bout row is editable at once
// (only kachinuki records bout by bout), so score each row then Finish once.
// The loser's point goes in first for the same reason as the individual bout.
async function scoreTeamEncounter(editor, page, i) {
  const rows = editor.locator('.team-sub-match');
  const n = await rows.count();
  const profile = TEAM_PROFILES[i % TEAM_PROFILES.length];
  for (let b = 0; b < n; b += 1) {
    const row = rows.nth(b);
    const aka = row.locator('.team-sub-match__side--aka');
    const shiro = row.locator('.team-sub-match__side--shiro');
    const outcome = profile[b % profile.length];
    if (outcome === 'draw') {
      await ipponButton(shiro, 'D').click();
      await settle(page, 120);
      await ipponButton(aka, 'M').click();
    } else {
      const winner = outcome === 'aka' ? aka : shiro;
      await ipponButton(winner, 'M').click();
      await settle(page, 120);
      await ipponButton(winner, 'K').click();
    }
    await settle(page, 150);
  }
}

// Engi is scored in FLAGS, not ippons: two counters whose total must be 1, 3
// or 5 (admin_scoring_engi.jsx:29-30, VALID_TOTALS), so a draw is unreachable
// by construction. Its commit is "Save result", not the two-tap Finish.
// Every split leaves the loser at least one flag: a pair that loses twice
// nil-nil would show a 0 in the Flags column the capture exists to explain.
const ENGI_SPLITS = [[3, 2], [4, 1], [1, 4], [2, 3]];

async function scoreEngiMatch(page, base, compId, i) {
  const editor = page.locator(EDITOR).first();
  const [aka, shiro] = ENGI_SPLITS[i % ENGI_SPLITS.length];
  for (let n = 0; n < aka; n += 1) {
    await editor.locator('[data-testid="engi-aka-inc"]').click();
    await settle(page, 100);
  }
  for (let n = 0; n < shiro; n += 1) {
    await editor.locator('[data-testid="engi-shiro-inc"]').click();
    await settle(page, 100);
  }
  // engi-submit is a one-tap commit ("Save result"), but it carries the same
  // "Finish + Start Next →" chaining when another match waits.
  await editor.locator('[data-testid="engi-submit"]').click();
  await settle(page, 600);
  await closeEditor(page);
}

async function scoreEveryEngiMatch(page, base, compId, startIndex, limit = 20) {
  let done = 0;
  for (let guard = 0; guard < limit; guard += 1) {
    await gotoScores(page, base, compId);
    const rows = scorableRows(page);
    if (!(await rows.count())) break;
    await openEditorForRow(page, rows.first());
    await scoreEngiMatch(page, base, compId, startIndex + done);
    done += 1;
  }
  return done;
}

// Play a Swiss competition out: score the open round, ask for the next one,
// repeat. POST /competitions/:id/swiss/generate-round is the real path
// (internal/mobileapp/handlers_swiss.go:48); it 409s while the current round
// still has an unscored match, which is why the scoring runs first.
async function playSwiss(api, page, base, compId, rounds, scoreRound) {
  let scored = 0;
  for (let r = 1; r <= rounds; r += 1) {
    scored += await scoreRound(scored);
    if (r < rounds) await api.post(`/api/competitions/${compId}/swiss/generate-round`);
  }
  return scored;
}

// ---------------------------------------------------------------------------
// The Lineups page (admin_lineup.jsx).
//
// Naming a fighter is a RENAME of the numbered blank member the team was
// seeded with, never a fresh mint: "rename, never a new mint while a blank
// slot with that number exists" (CLAUDE.md). That is exactly why this cannot
// be an API call - a lineup PUT carrying names alone leaves team-members.yaml
// holding `name: ""` and every number chip renders empty.
// ---------------------------------------------------------------------------
async function enterLineup(api, page, base, compId, names) {
  await page.goto(`${base}/admin/competition/${compId}/lineups`, { waitUntil: 'domcontentloaded' });
  await page.locator('[data-testid="lineup-form-root"]').waitFor({ state: 'visible', timeout: 20000 });
  await settle(page, 400);

  // The Team dropdown lists teams in roster order, but the slot numbers come
  // from the DRAW, so the first team in the list is not necessarily T1. Select
  // the team that drew position 1 so the capture shows T1.1-T1.7.
  const teamSelect = page.locator('select').first();
  const options = await teamSelect.locator('option').evaluateAll((os) => os.map((o) => o.value));
  let teamId = null;
  for (const value of options) {
    await teamSelect.selectOption(value);
    await settle(page, 500);
    const first = await page.locator('[data-testid^="squad-member-"]').first().innerText();
    if (first.trim().startsWith('T1.')) { teamId = value; break; }
  }
  // Falling through used to leave teamId holding whichever team happened to be
  // last, and the capture would then show a different team's numbers - self
  // consistent, so not obviously wrong to a reviewer. Say so instead.
  if (!teamId) {
    throw new Error('no team in the dropdown drew position 1 (no T1.x member), so the ' +
      'lineup would be entered against the wrong team');
  }

  // Rename the blank numbered members, one at a time: the row swaps its label
  // for an input plus Save/Cancel (lineup_rename.jsx).
  for (let i = 0; i < names.length; i += 1) {
    const row = page.locator('[data-testid^="squad-member-"]').nth(i);
    await row.locator('button').filter({ hasText: /^Rename$/ }).click();
    const input = row.locator('input.input');
    await input.waitFor({ state: 'visible', timeout: 10000 });
    await input.fill(names[i]);
    await row.locator('button').filter({ hasText: /^Save$/ }).click();
    await page.locator('[data-testid^="squad-member-"]').nth(i).locator('button')
      .filter({ hasText: /^Rename$/ }).waitFor({ state: 'visible', timeout: 10000 });
    await settle(page, 200);
  }

  // Pick each renamed member into its position BY ID: a position written with
  // a name and no id is the failure this whole detour exists to avoid.
  //
  // Always the FIRST offered member, never the i-th: a member already placed
  // at another position is dropped from every other picker
  // (rosterWithoutPlacedElsewhere, lineup_resolver.jsx), so the list shortens
  // by one after each pick and an index would drift past the end.
  for (const position of POSITIONS) {
    const select = page.locator(`[data-testid="lineup-position-${position}"]`);
    const value = await select.locator('option').nth(1).getAttribute('value');
    await select.selectOption(value);
    await settle(page, 250);
  }

  await page.locator('button').filter({ hasText: /^Save lineup$/ }).click();
  await page.locator('button').filter({ hasText: /^Save lineup$/ }).waitFor({ state: 'visible', timeout: 15000 });
  await settle(page, 800);

  // Read the lineup back off the server before anything downstream trusts it.
  // A save that half-lands leaves a page that still LOOKS right, and the
  // number chips are only missing once the capture is already on disk.
  const saved = await api.get(`/api/competitions/${compId}/teams/${teamId}/lineups/0`);
  const ids = Object.values(saved?.memberIds || {}).filter(Boolean);
  if (ids.length !== names.length) {
    throw new Error(`${compId}: lineup for ${teamId} saved ${ids.length} member ids, expected ${names.length}: ` +
      JSON.stringify(saved));
  }
  return teamId;
}

// ---------------------------------------------------------------------------
// Local read-back checks.
//
// These exist because lib/fixture.mjs's assertBoutPoints cannot see the
// failure it describes in a Swiss competition (see the comment on its call
// site), and each was RED-VERIFIED: pointed at a competition seeded the wrong
// way - one whose encounters went through /quick-score - and watched to fail.
// An assertion nobody has seen fail is not a gate. All are candidates for
// lib/fixture.mjs.
// ---------------------------------------------------------------------------
// pool-matches.csv has to be PARSED, not grepped, and lib/fixture.mjs's
// csvRows is the reader for it - its header says why. A pattern written for
// the SubResults JSON silently never matches, which produces a check that
// passes on exactly the fixture it exists to reject; these two were
// red-verified against a quick-scored competition.
function poolMatchRecords(dataDir, compId) {
  const p = path.join(dataDir, 'competitions', compId, 'pool-matches.csv');
  if (!fs.existsSync(p)) throw new Error(`${compId}: no pool-matches.csv`);
  const [header, ...rows] = csvRows(fs.readFileSync(p, 'utf8'));
  if (!header) throw new Error(`${compId}: pool-matches.csv is empty`);
  return rows.filter((r) => r.length === header.length)
    .map((r) => Object.fromEntries(header.map((h, i) => [h, r[i]])));
}

// A team encounter's IV / IL / IT / PW / PL are tallied from its SUB-BOUTS, so
// a file whose sub-bouts carry no ippons renders a table of zeros. That is
// exactly the shape quick-scoring writes: a row per position with a winner and
// `ipponsA: null`.
function assertTeamSubBouts(dataDir, compId) {
  const completed = poolMatchRecords(dataDir, compId).filter((m) => m.Status === 'completed');
  if (!completed.length) throw new Error(`${compId}: no completed encounter to check`);
  let scoredBouts = 0;
  for (const m of completed) {
    let subs;
    try {
      subs = JSON.parse(m.SubResults || '[]');
    } catch {
      throw new Error(`${compId}: match ${m.MatchIdx} has unreadable SubResults`);
    }
    if (!subs.length) {
      throw new Error(`${compId}: match ${m.MatchIdx} records no sub-bouts, so IV/PW render as zero`);
    }
    scoredBouts += subs.filter((s) => (s.ipponsA || []).length || (s.ipponsB || []).length).length;
  }
  if (!scoredBouts) {
    throw new Error(`${compId}: every sub-bout carries null ippons - the encounters were ` +
      'quick-scored rather than fought through the team sheet, so PW/PL render as zero');
  }
  return scoredBouts;
}

// An INDIVIDUAL match keeps its points in the top-level IpponsA / IpponsB
// columns (the SubResults blob above is the team shape), and they are what the
// pool's PW / PL and the head-to-head letters are drawn from.
function assertIndividualIppons(dataDir, compId) {
  const completed = poolMatchRecords(dataDir, compId).filter((m) => m.Status === 'completed');
  if (!completed.length) throw new Error(`${compId}: no completed pool match to check`);
  const withPoints = completed.filter((m) => (m.IpponsA || '').trim() || (m.IpponsB || '').trim());
  if (!withPoints.length) {
    throw new Error(`${compId}: no completed pool match records an ippon - PW/PL will be zero ` +
      'and the head-to-head grid will be blank');
  }
  return withPoints.length;
}

// Engi scores in flags, in their own two columns; every other column of an
// untouched row is already full of digits, so only these two can be read.
function assertEngiFlags(dataDir, compId) {
  const completed = poolMatchRecords(dataDir, compId).filter((m) => m.Status === 'completed');
  if (!completed.length) throw new Error(`${compId}: no completed engi match to check`);
  const total = completed.reduce((sum, m) => sum + Number(m.FlagsA || 0) + Number(m.FlagsB || 0), 0);
  if (!total) {
    throw new Error(`${compId}: every completed match records 0 flags - the engi counters ` +
      'never landed, so V/Flags render as zero');
  }
  return total;
}

// Exported so the checks themselves can be red-verified against a saved
// quick-scored fixture: an assertion nobody has watched fail is not a gate.
// The registry only reads `families` and `recipes`, so extra exports are inert.
export { assertTeamSubBouts, assertEngiFlags, assertIndividualIppons };

// ---------------------------------------------------------------------------
// The family.
// ---------------------------------------------------------------------------
export const families = {
  scored: {
    seed: async ({ api, base, browser }) => {
      await api.tournament({
        name: 'London Cup 2026',
        date: '19-09-2026',
        venue: 'London',
        durationDays: 2,
        courts: ['A', 'B'],
      });

      // A team KNOCKOUT competition, draw generated but NOT started: the
      // Lineups capture shows the "Draw ready" badge and the Start control.
      await api.competition(TEAM_COMP, 'Team Championship', {
        teamSize: 5, format: 'knockout', courts: ['A', 'B'],
        numberPrefix: 'T', date: '19-09-2026', startTime: '10:53',
      });
      await api.participants(TEAM_COMP, TEAMS);
      await api.generateDraw(TEAM_COMP);

      await api.competition(SWISS_TEAM, 'Team Swiss', {
        teamSize: 3, format: 'swiss', swissRounds: 2, courts: ['A', 'B'],
        date: '19-09-2026', startTime: '11:00',
      });
      await api.participants(SWISS_TEAM, SWISS_TEAMS);
      await api.start(SWISS_TEAM);

      await api.competition(SWISS_ENGI, 'Engi Swiss', {
        teamSize: 0, format: 'swiss', swissRounds: 2, engi: true, courts: ['A', 'B'],
        date: '20-09-2026', startTime: '12:00',
      });
      await api.participants(SWISS_ENGI, ENGI_PAIRS);
      await api.start(SWISS_ENGI);

      await api.competition(POOLS_COMP, 'Women up to 2D', {
        teamSize: 0, format: 'mixed', poolSize: 3, poolWinners: 2, roundRobin: true,
        courts: ['A', 'B'], numberPrefix: 'A', date: '20-09-2026', startTime: '09:00',
      });
      await api.participants(POOLS_COMP, POOL_NAMES.map((name, i) => ({
        name, dojo: POOL_DOJOS[i % POOL_DOJOS.length],
      })));
      await api.start(POOLS_COMP);

      const context = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
      const page = await context.newPage();
      let lineupTeam = '';
      try {
        await loginAdmin(page, base);
        lineupTeam = await enterLineup(api, page, base, TEAM_COMP, LINEUP_NAMES);

        await playSwiss(api, page, base, SWISS_TEAM, 2, (done) =>
          scoreEveryMatch(page, base, SWISS_TEAM, (editor, p, i) =>
            scoreTeamEncounter(editor, p, done + i), 8));

        await playSwiss(api, page, base, SWISS_ENGI, 2, (done) =>
          scoreEveryEngiMatch(page, base, SWISS_ENGI, done, 8));

        await scoreEveryMatch(page, base, POOLS_COMP, scoreIndividualBout, 60);
        await api.post(`/api/competitions/${POOLS_COMP}/complete`);
      } finally {
        await context.close();
      }

      return {
        teamComp: TEAM_COMP, swissTeam: SWISS_TEAM, swissEngi: SWISS_ENGI, pools: POOLS_COMP,
        // Which team holds the entered lineup. The draw decides the slot
        // numbers, so this is not "the first team in the roster" and the
        // capture has to ask for it by id.
        lineupTeam,
      };
    },
  },
};

export const recipes = [
  {
    // The Lineups tab, with a completed fighting order and the Team members
    // list beneath it. The whole page fits in 1585x1212 with room to spare, so
    // this is a plain viewport capture at DPR 1 rather than a full-page one:
    // full-page reports the layout width (1600), never the 15px a scrollbar
    // took off the original.
    name: 'team-lineup',
    server: 'mobile',
    family: 'scored',
    route: `/admin/competition/${TEAM_COMP}/lineups`,
    viewport: { width: 1585, height: 1212 },
    dpr: 1,
    capture: 'viewport',
    setup: ({ page, base }) => loginAdmin(page, base),
    waitFor: '[data-testid="lineup-form-root"]',
    // The Team dropdown opens on the first team in ROSTER order, which is not
    // the one whose lineup was entered: slot numbers come from the draw.
    drive: async ({ page, fixture }) => {
      await page.locator('select').first().selectOption(fixture.lineupTeam);
      await page.waitForFunction(
        () => {
          const s = document.querySelector('[data-testid="lineup-position-senpo"]');
          return !!s && !!s.value;
        },
        { timeout: 15000 },
      );
      await settle(page, 400);
    },
    // The shared check proves a memberIds block exists; the count proves all
    // five positions carry one, which is what puts a chip on every row.
    assert: ({ dataDir }) => {
      const { ids } = assertLineupIds(dataDir, TEAM_COMP);
      if (ids !== LINEUP_NAMES.length) {
        throw new Error(`${TEAM_COMP}: lineups.yaml carries ${ids} member ids, expected ` +
          `${LINEUP_NAMES.length} - a position without one renders no competitor number`);
      }
    },
  },
  {
    // The whole standings card, not just its <table>. The committed file was
    // the grid alone, but its alt text on team-tournaments.md promises "a
    // caption reading Ranked by: team wins, IV, PW, head-to-head" - and that
    // caption is a sibling DIV after </table>, so the old crop could never
    // contain it. The image was quietly contradicting its own description.
    name: 'swiss-standings-team',
    server: 'mobile',
    family: 'scored',
    route: `/competition/${SWISS_TEAM}/swiss`,
    viewport: { width: 900, height: 900 },
    dpr: 1,
    capture: { selector: '.pool' },
    waitFor: 'table.pool__table tbody tr td',
    // Stricter than lib/fixture.mjs's assertBoutPoints: this also rejects a
    // match that recorded NO sub-bouts at all, which for a team encounter
    // means the editor never wrote one.

    // JSON-shaped, and a Swiss competition's only result file is a CSV whose
    // embedded JSON has its quotes doubled, so it PASSES on a quick-scored
    // fixture - verified against one. assertTeamSubBouts is the real gate.
    assert: ({ dataDir }) => { assertTeamSubBouts(dataDir, SWISS_TEAM); },
  },
  {
    // The engi Swiss standings with their winner banner. The page footer is in
    // shot, so this is the whole viewer page, not a crop.
    name: 'swiss-standings-engi',
    server: 'mobile',
    family: 'scored',
    route: `/competition/${SWISS_ENGI}/swiss`,
    viewport: { width: 769, height: 774 },
    dpr: 1,
    capture: 'viewport',
    waitFor: 'table.pool__table tbody tr td',
    assert: ({ dataDir }) => { assertEngiFlags(dataDir, SWISS_ENGI); },
  },
  {
    // The public Pools tab, full-page at DPR 2. 2530 = 1265 x 2, which is the
    // content width a 1280 window has once its scrollbar is taken off - the
    // original's layout width. A full-page capture reports the LAYOUT width,
    // so the 1265 has to be asked for: at 1280 it comes out 2560.
    name: 'mobile-pool-standings',
    server: 'mobile',
    family: 'scored',
    route: `/competition/${POOLS_COMP}/pools`,
    viewport: { width: 1265, height: 900 },
    dpr: 2,
    capture: 'fullPage',
    waitFor: '.pool',
    // assertBoutPoints is NOT used here. It reads the SubResults column, which
    // only a TEAM competition fills, so on this individual competition it would
    // iterate nothing and pass regardless. assertIndividualIppons reads the
    // top-level ippon columns, which is where this competition's points live.
    assert: ({ dataDir }) => {
      assertIndividualIppons(dataDir, POOLS_COMP);
    },
  },
];
