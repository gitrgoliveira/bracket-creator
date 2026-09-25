// Knockout bracket captures: the admin Bracket page and the public Bracket tab.
//
// Two families, because the two subjects need different fixtures and neither
// should pay for the other's seed:
//
//   knockoutDraw  five competitors, draw generated and started, nothing scored,
//                 captured on the admin Bracket page. The subject is the SHAPE
//                 the draw produces: one first-round bout, three competitors
//                 who start a round later, and a pair that meets for the first
//                 time in round two.
//   knockoutPlay  ten competitors, the first-round bouts and two
//                 quarterfinals scored, so winners have moved on and the tree
//                 visibly fills in; both of its captures are the public
//                 Bracket tab (knockout-bracket-in-play says why the admin page
//                 cannot frame this tree). The results are entered by DRIVING THE
//                 SCORE EDITOR, not over HTTP, for the reason lib/api.mjs gives:
//                 a result written over the API is not what an operator
//                 produces, and a capture must show state the application made.
//
// Neither family claims web-mobile/js/bracket.jsx, although every capture here
// renders it. That module also exports the score-string and middle-mark
// primitives the encho, court-board and standings captures render, and a claim
// NARROWS a change to the claiming families (lib/scope.mjs): claiming it would
// let a bracket.jsx edit skip those captures. Left unclaimed, a change to it
// selects every family, these two included.
import fs from 'node:fs';
import path from 'node:path';
import { settle, withAdminPage } from '../lib/ui.mjs';
import { EDITOR, finishMatch, startMatch } from '../lib/editor.mjs';
import { SCORE_EDITOR_SOURCES, VIEWER_SOURCES } from '../lib/scope.mjs';

// Competition ids at module level so each recipe's `route` can name them
// (run.mjs concatenates `base + route` when the recipe is declared).
const FIVE_ID = 'open-knockout';
const TEN_ID = 'mens-knockout';

const FIVE = [
  ['Kenta Morita', 'Mumeishi'], ['Laura Schmidt', 'Berlin Kenyu'], ['Tom Harris', 'Hizen Dojo'],
  ['Sofia Bianchi', 'Milano Kendo'], ['Daichi Ono', 'Mumeishi'],
].map(([name, dojo]) => ({ name, dojo }));

const TEN = [
  ['Hiroshi Kondo', 'Kenshinkan'], ['James Carter', 'Thames Dojo'], ['Anna Fischer', 'Berlin Kenyu'],
  ['Mateo Garcia', 'Madrid Dojo'], ['Ren Aoki', 'Seishinkan'], ['Oliver Grant', 'Hizen Dojo'],
  ['Lucas Martin', 'Paris Dojo'], ['Takumi Endo', 'Kenshinkan'], ['Erik Lindqvist', 'Nordic Kendo'],
  ['Sam Whitfield', 'Thames Dojo'],
].map(([name, dojo]) => ({ name, dojo }));

// How many results knockoutPlay records, in schedule order. A knockout is
// scheduled in match-number order, deepest round first, so this is the two
// first-round bouts (M1, M2) and the first two quarterfinals (M3, M4), whose
// winners then meet in a semifinal with both sides known.
const PLAYED = 4;

// An ippon button is one CHARACTER (M/K/D/T/H), so match it exactly.
function ipponButton(scope, waza) {
  return scope.locator('button.ipt-btn').filter({ hasText: new RegExp(`^${waza}$`) }).first();
}

// One bout: the winner takes two ippons, and every other bout the loser takes
// one first, so the tree carries a mix of 2-0 and 2-1 scorelines. The loser's
// point goes in FIRST: a side reaching two ippons decides the bout and the
// editor then disables the other side's buttons.
async function scoreBout(editor, page, i) {
  const winner = i % 2 === 0 ? 'aka' : 'shiro';
  const loser = winner === 'aka' ? 'shiro' : 'aka';
  if (i % 2 === 1) {
    await ipponButton(editor.locator(`.sb-side--${loser}`), 'K').click();
    await settle(page, 100);
  }
  const win = editor.locator(`.sb-side--${winner}`);
  await ipponButton(win, i % 3 === 0 ? 'M' : 'K').click();
  await settle(page, 100);
  await ipponButton(win, i % 3 === 1 ? 'D' : 'M').click();
  await settle(page, 100);
}

// Open the competition's first scorable match and play `count` of them through
// ONE editor: every "Finish + Start Next" completes the open match and starts
// the next one on the same shiaijo, so the editor never has to be reopened from
// the list beneath its backdrop. The last Finish starts the next match too,
// which leaves it running: the capture shows a bracket mid-play.
async function playInOrder(page, base, compId, count) {
  await page.goto(`${base}/admin/competition/${compId}/scores`, { waitUntil: 'domcontentloaded' });
  const row = page.locator('.score-edit-row')
    .filter({ has: page.locator('button', { hasText: /^Score$/ }) }).first();
  await row.waitFor({ state: 'visible', timeout: 15000 });
  await row.locator('button').filter({ hasText: /^Score$/ }).first().click();
  const editor = page.locator(EDITOR).first();
  await editor.waitFor({ state: 'visible', timeout: 15000 });
  await startMatch(page);
  for (let i = 0; i < count; i += 1) {
    await scoreBout(editor, page, i);
    await finishMatch(page);
  }
}

// The real bouts of a bracket.json: a structural bye is hidden or one-sided,
// and advances its competitor without a result, so it is not a bout.
function bouts(dataDir, compId) {
  const p = path.join(dataDir, 'competitions', compId, 'bracket.json');
  if (!fs.existsSync(p)) throw new Error(`${compId}: no bracket.json`);
  const { rounds = [] } = JSON.parse(fs.readFileSync(p, 'utf8'));
  return rounds.flat().filter((m) => m && !m.hidden && m.sideA && m.sideB);
}

// Refuse a fixture whose results carry no points: the scorelines are half of
// what the in-play captures show, and a completed match with no ippons renders
// a bare winner.
function assertPlayed(dataDir, compId) {
  const completed = bouts(dataDir, compId).filter((m) => m.status === 'completed');
  if (completed.length < PLAYED) {
    throw new Error(`${compId}: ${completed.length} bracket matches completed, expected ${PLAYED}`);
  }
  const bare = completed.filter((m) => !(m.ipponsA || []).length && !(m.ipponsB || []).length);
  if (bare.length) {
    throw new Error(`${compId}: ${bare.length} completed match(es) carry no ippons, so their ` +
      'scores would render blank');
  }
}

// The admin crop is the bracket card, which scrolls sideways when the tree is
// wider than it. An element screenshot of a scrolled box silently drops the
// columns past its right edge, so refuse to take one.
async function assertBracketFits(page) {
  const { scroll, client } = await page.locator('.bracket-canvas').first()
    .evaluate((el) => ({ scroll: el.scrollWidth, client: el.clientWidth }));
  if (scroll > client) {
    throw new Error(`the bracket card is ${client}px wide but its tree needs ${scroll}px, ` +
      'so the capture would cut off its right-hand rounds');
  }
}

export const families = {
  knockoutDraw: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the admin competition page and its
    // Bracket section (admin_competition_bracket.jsx). bracket.jsx is left
    // unclaimed on purpose; see the note at the top of this file.
    sources: ['web-mobile/js/admin_competition'],
    seed: async ({ api }) => {
      await api.tournament({ name: 'Autumn Taikai 2026', date: '10-10-2026', courts: ['A'] });
      await api.competition(FIVE_ID, 'Open Knockout', {
        format: 'knockout', courts: ['A'], date: '10-10-2026', numberPrefix: 'K',
      });
      await api.participants(FIVE_ID, FIVE);
      await api.generateDraw(FIVE_ID);
      await api.start(FIVE_ID);
    },
  },

  knockoutPlay: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the public competition page whose
    // Bracket tab both captures show, and the score editor the seed drives.
    sources: [...VIEWER_SOURCES, ...SCORE_EDITOR_SOURCES],
    seed: async ({ api, base, browser }) => {
      await api.tournament({ name: 'Autumn Taikai 2026', date: '10-10-2026', courts: ['A'] });
      // K, not M: an M-prefixed competitor chip ("M1") reads as a match number
      // beside the bracket's own M1/M2 match labels.
      await api.competition(TEN_ID, "Men's Knockout", {
        format: 'knockout', courts: ['A'], date: '10-10-2026', numberPrefix: 'K',
      });
      await api.participants(TEN_ID, TEN);
      await api.generateDraw(TEN_ID);
      await api.start(TEN_ID);
      await withAdminPage(browser, { width: 1600, height: 1000 }, async (page) => {
        await playInOrder(page, base, TEN_ID, PLAYED);
      });
    },
  },
};

// The tree renders once unpositioned, measures its cards, then re-renders with
// every column absolutely placed and the connectors drawn. This class is the
// second render; waiting on anything earlier photographs the first.
const TREE_READY = '.bc-round-matches--abs';

export const recipes = [
  {
    // The admin Bracket page, cropped to the bracket card. The scoring panel
    // sits below the card, so the card takes the whole content width.
    name: 'knockout-bracket-five',
    family: 'knockoutDraw',
    route: `/admin/competition/${FIVE_ID}/bracket`,
    viewport: { width: 1200, height: 900 },
    capture: { selector: '.bracket-canvas' },
    auth: 'admin',
    waitFor: TREE_READY,
    drive: async ({ page }) => {
      await settle(page, 400);
      await assertBracketFits(page);
    },
  },
  {
    // The PUBLIC Bracket tab at a desktop width: a draw part-way through, as
    // spectators follow it (the tab widens its shell to 1440px, so a
    // sixteen-slot tree shows whole). 1265 is the width of
    // mobile-pool-standings, the image this sits beside.
    name: 'knockout-bracket-in-play',
    family: 'knockoutPlay',
    route: `/competition/${TEN_ID}/bracket`,
    viewport: { width: 1265, height: 900 },
    capture: 'fullPage',
    waitFor: TREE_READY,
    drive: async ({ page }) => {
      await settle(page, 400);
      await assertBracketFits(page);
    },
    assert: ({ dataDir }) => { assertPlayed(dataDir, TEN_ID); },
  },
  {
    // The same Bracket tab at the width of the spectator page's other viewer
    // captures. Here the tree is wider than the page and scrolls sideways
    // behind the tab's right-edge fade, which is how a phone shows it; the tab
    // opens scrolled to the running match.
    name: 'viewer-bracket',
    family: 'knockoutPlay',
    route: `/competition/${TEN_ID}/bracket`,
    viewport: { width: 815, height: 900 },
    capture: 'fullPage',
    waitFor: TREE_READY,
    drive: async ({ page }) => { await settle(page, 400); },
    assert: ({ dataDir }) => { assertPlayed(dataDir, TEN_ID); },
  },
];
