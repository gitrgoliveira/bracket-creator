// The SPECTATOR's league journey, on the PUBLIC project (a 390x844 phone, no
// sign-in): a competitor, a coach or the public reading a team league on the
// competition page (League tab, Overview, Awards) and on the venue's TV board
// (/display?court=X), while a tie waits to be settled, once it is settled, and
// after the organiser takes the settling back.
//
// The test's own `page` is the phone. The league is seeded and scored from a
// SECOND context opened with the operator's iPad (OPERATOR_DEVICE), through the
// interface, so the tournament is OFFICIATED: a spectator reads, never scores
// (self-run public scoring is a different journey, mp-yqxn.5).
//
// Every league here is four teams of three where three of them (Hayabusa, Inoshishi,
// Kitsune) beat each other in a ring and all beat the fourth (Tanuki) by the
// same scoreline, so the table ties them for 1st to 3rd: a consequential tie
// at the podium.
//
//   1. the tie waits: the League tab and the TV board while the banner is up,
//      the Awards tab not yet offered, then a tie-breaker scheduled and what
//      the board and the League tab say of it
//   2. the tie accepted: "Final standings", the Winner badge and the podium on
//      the phone, and the board
//   3. N1 and F1, test.fixme until the fix: the Overview names no winner while
//      the tie waits, and after an Undo nothing reads as final
//
// Not serial: each test signs the operator in (or creates the tournament when
// this file's server has none), seeds a league of its own on a shiaijo of its
// own, and keeps its own screenshots and audit rows, so one failing test hides
// nothing else here. What is asserted is what the app promises; how the rest
// reads (does a shared rank read as shared, what the board says while a tie
// waits) is recorded as AUDIT rows and judged from the screenshots.
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { enterTournament } from '../fixtures/setup.mjs';
import { openTvBoard, openViewer, tvBoard } from '../fixtures/public.mjs';
import {
  acceptSharedRanks, awardsTabOffered, leagueRanks, openLeague, openNumberedRow, openViewerAwards, openViewerLeague,
  overviewWinner, playEncounter, readBanner, readLeagueTable, readNumberedRows, readPodium, runTiebreaker, seedLeague,
  undoSharedRanks,
} from '../fixtures/league.mjs';

// Enough shiaijo for each test here to have one of its own.
const LEAGUE_CUP = { name: 'League Cup', courts: 4 };

// Four teams of three, four dojos. The first three ring-tie, the fourth loses
// to all of them.
const teamsOf = (tag) => ({
  ring: [`${tag} Hayabusa`, `${tag} Inoshishi`, `${tag} Kitsune`],
  last: `${tag} Tanuki`,
  entrants: [
    [`${tag} Hayabusa`, 'Kita Dojo'], [`${tag} Inoshishi`, 'Minami Dojo'],
    [`${tag} Kitsune`, 'Higashi Dojo'], [`${tag} Tanuki`, 'Nishi Dojo'],
  ],
});

// A recorder per test, flushed by afterAll to the audit.json in the test's own
// folder (the one its screenshots go to).
const audits = [];
function recorder(journey, folder) {
  const log = auditLog(folder);
  audits.push(log);
  return { record: (row) => log.record({ journey, ...row }) };
}

// The operator's iPad on its own context, signed in.
async function operatorPage(browser, baseURL) {
  const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL });
  const op = await ctx.newPage();
  await enterTournament(op, LEAGUE_CUP);
  return { ctx, op };
}

// `winner` beats `loser`, 2 bouts to none, on the league's Pools page, from
// the numbered row of that pair; returns once the table counts the encounter.
async function beat(op, winner, loser, playedAfter) {
  const rows = await readNumberedRows(op);
  const row = rows.find((r) => (r.shiro === winner && r.aka === loser) || (r.shiro === loser && r.aka === winner));
  expect(row, `a numbered row for ${winner} v ${loser}`).toBeTruthy();
  const side = row.shiro === winner ? 'shiro' : 'aka';
  const ed = await openNumberedRow(op, { shiro: row.shiro, aka: row.aka });
  await playEncounter(ed, [side, side, 'tie']);
  await expect.poll(async () => (await readLeagueTable(op)).played).toBe(playedAfter);
}

// Seed a league of `teams` on `court` and play its six encounters so the ring
// ties for 1st to 3rd; leaves the operator on the league's Pools page with the
// tie-break banner up.
async function seedRingTie(op, { name, court, teams }) {
  const id = await seedLeague(op, {
    name, kind: 'team', entrants: teams.entrants, teamSize: 3, teamMatchType: 'fixed', courts: [court],
  });
  await openLeague(op, id);
  const [a, b, c] = teams.ring;
  const sequence = [[a, b], [b, c], [c, a], [a, teams.last], [b, teams.last], [c, teams.last]];
  for (const [i, [winner, loser]] of sequence.entries()) await beat(op, winner, loser, i + 1);
  await expect.poll(async () => (await readBanner(op)).shown, { message: 'the tie-break banner' }).toBe(true);
  return id;
}

// What the TV board of `court` reads (the whole screen's text, squashed) and
// whether it shows the "All matches completed" state.
async function readBoard(page, court) {
  await openTvBoard(page, court);
  return {
    text: (await tvBoard(page).innerText()).replace(/\s+/g, ' ').trim(),
    allCompleted: (await page.getByTestId('display-all-completed').count()) > 0,
  };
}

test.describe('league on a phone', () => {
  test.afterAll(() => audits.forEach((a) => a.flush()));

  test('a tie at the podium waits: the League tab and the TV board while the banner is up, then with a tie-breaker scheduled', async ({ page, browser, baseURL }) => {
    test.setTimeout(480_000);
    const shot = journeyShots('league-public/tie-waits');
    const { record } = recorder('tie-waits', 'league-public/tie-waits');
    const teams = teamsOf('T');
    const { ctx, op } = await operatorPage(browser, baseURL);
    let id;
    let adminRanks;
    let adminRows;

    try {
      await test.step('the operator plays the ring: three teams level, one tie-break banner', async () => {
        id = await seedRingTie(op, { name: 'Tie League', court: 'A', teams });
        const banner = await readBanner(op);
        adminRanks = await leagueRanks(op);
        adminRows = await readNumberedRows(op);
        record({ step: 'admin', action: 'Pools once the ring is played', ranks: adminRanks, banner });
        expect(banner.groups).toHaveLength(1);
        expect([...banner.groups[0].teams].sort()).toEqual([...teams.ring].sort());
        expect(adminRows).toHaveLength(6);
      });

      await test.step('the phone reads the same table on the League tab: every encounter counted, the last team below the ring', async () => {
        await openViewerLeague(page, id);
        const table = await readLeagueTable(page);
        const ranks = await leagueRanks(page);
        const ringRanks = teams.ring.map((t) => ranks[t]);
        record({
          step: 'public', action: 'League tab with the tie waiting', table, ranks,
          ringShareARank: new Set(ringRanks).size === 1, offersAwards: await awardsTabOffered(page),
        });
        await shot(page, 'league-tab-tie-waits', { fullPage: true });
        expect(table.played).toBe(6);
        expect(table.total).toBe(6);
        // The phone and the desk read one table.
        expect(ranks).toEqual(adminRanks);
        for (const t of teams.ring) expect(ranks[t]).toBeLessThan(ranks[teams.last]);
        // A tie that is not settled is not a result: nothing is final yet.
        expect(table.heading).toBe('Standings');
        expect(await awardsTabOffered(page)).toBe(false);
      });

      await test.step('the phone: the Overview while the tie waits (recorded; N1 asserts it)', async () => {
        await openViewer(page, id);
        const winners = await overviewWinner(page).count();
        record({
          step: 'public', action: 'Overview with the tie waiting', winnerBadges: winners,
          winnerNamed: winners ? (await overviewWinner(page).first().innerText()).replace(/\s+/g, ' ').trim() : '',
        });
        await shot(page, 'overview-tie-waits', { fullPage: true });
      });

      await test.step('the TV board with every regular match played and nothing scheduled', async () => {
        const board = await readBoard(page, 'A');
        record({ step: 'display', action: 'board with the tie waiting, no tie-breaker yet', ...board });
        await shot(page, 'board-tie-waits');
        // The board is read, not judged here: it is recorded for the review
        // (does it say the shiaijo is done while the tie is open?).
      });

      await test.step('the operator runs a tie-breaker for the ring', async () => {
        await runTiebreaker(op, teams.ring);
      });

      await test.step('the phone: the tie-breaker bouts are listed on the League tab and the table stays unfinished', async () => {
        await openViewerLeague(page, id);
        const rows = await readNumberedRows(page);
        const table = await readLeagueTable(page);
        record({ step: 'public', action: 'League tab with a tie-breaker scheduled', numberedRows: rows, table, offersAwards: await awardsTabOffered(page) });
        await shot(page, 'league-tab-tiebreaker-scheduled', { fullPage: true });
        expect(rows.length).toBeGreaterThan(adminRows.length);
        const ringNames = new Set(teams.ring);
        const extra = rows.filter((r) => r.num > adminRows.length);
        expect(extra.length).toBeGreaterThan(0);
        for (const r of extra) {
          expect(ringNames.has(r.shiro) && ringNames.has(r.aka), `tie-breaker bout ${r.shiro} v ${r.aka} is between ring teams`).toBe(true);
        }
        expect(table.heading).toBe('Standings');
        expect(await awardsTabOffered(page)).toBe(false);
      });

      await test.step('the TV board shows the tie-breaker as the next bout: the shiaijo is not done', async () => {
        const board = await readBoard(page, 'A');
        record({ step: 'display', action: 'board with a tie-breaker scheduled', ...board });
        await shot(page, 'board-tiebreaker-scheduled');
        expect(board.allCompleted).toBe(false);
        expect(teams.ring.some((t) => board.text.includes(t)), 'the board names a ring team').toBe(true);
      });
    } finally {
      await ctx.close();
    }
  });

  test('N1: the Overview names no winner while a tie at the podium waits', async ({ page, browser, baseURL }) => {
    test.fixme(true, 'N1: the public Overview shows a Winner badge (the first team of a three-way tie) while the tie-break banner is still up');
    test.setTimeout(480_000);
    const teams = teamsOf('N');
    const { ctx, op } = await operatorPage(browser, baseURL);
    try {
      const id = await seedRingTie(op, { name: 'Winner Too Early', court: 'C', teams });
      await openViewer(page, id);
      await expect(page.locator('.viewer__body')).toBeVisible();
      expect(await overviewWinner(page).count()).toBe(0);
    } finally {
      await ctx.close();
    }
  });

  test('shared ranks accepted: the phone reads Final standings, the Awards tab and the TV board reads the shiaijo as finished', async ({ page, browser, baseURL }) => {
    test.setTimeout(480_000);
    const shot = journeyShots('league-public/accepted');
    const { record } = recorder('accepted', 'league-public/accepted');
    const teams = teamsOf('U');
    const { ctx, op } = await operatorPage(browser, baseURL);
    let id;

    try {
      await test.step('the operator plays the ring and accepts the shared ranks', async () => {
        id = await seedRingTie(op, { name: 'Shared Ranks League', court: 'B', teams });
        const waiting = await leagueRanks(op);
        const answer = await acceptSharedRanks(op);
        record({ step: 'admin', action: 'Accept shared ranks', status: answer.status, asked: answer.asked, ranksBefore: waiting });
        expect(answer.ok).toBe(true);
      });

      await test.step('the phone: Final standings, the ring above the last team', async () => {
        await openViewerLeague(page, id);
        const table = await readLeagueTable(page);
        const ranks = await leagueRanks(page);
        const ringRanks = teams.ring.map((t) => ranks[t]);
        // AUDIT: the three teams were accepted as SHARING their place; the
        // rank column is read here, not judged (N2).
        record({
          step: 'public', action: 'League tab once the shared ranks are accepted', table, ranks,
          ringShareARank: new Set(ringRanks).size === 1,
        });
        await shot(page, 'league-tab-accepted', { fullPage: true });
        expect(table.heading).toBe('Final standings');
        expect(table.played).toBe(6);
        for (const t of teams.ring) expect(ranks[t]).toBeLessThan(ranks[teams.last]);
      });

      await test.step('the phone: the Awards tab is offered and reads the podium', async () => {
        await openViewerAwards(page, id);
        const podium = await readPodium(page);
        record({
          step: 'public', action: 'Awards tab once the shared ranks are accepted', podium,
          ringOnTheFirstPlace: podium.places.filter((p) => p.place === 1).length,
        });
        await shot(page, 'awards-accepted', { fullPage: true });
        expect(podium.places.length).toBeGreaterThan(0);
        const firstPlace = podium.places.filter((p) => p.place === 1).map((p) => p.name);
        expect(firstPlace.length).toBeGreaterThan(0);
        for (const n of firstPlace) expect(teams.ring.some((t) => n.includes(t))).toBe(true);
        expect(podium.places.map((p) => p.name)).not.toContain(teams.last);
      });

      await test.step('the TV board: the shiaijo reads as finished', async () => {
        const board = await readBoard(page, 'B');
        record({ step: 'display', action: 'board once the shared ranks are accepted', ...board });
        await shot(page, 'board-accepted');
        expect(board.allCompleted).toBe(true);
      });
    } finally {
      await ctx.close();
    }
  });

  test('F1: after an Undo of the shared ranks nothing on the phone or the TV board reads as final', async ({ page, browser, baseURL }) => {
    test.fixme(true, 'F1: accepted shared ranks cannot be undone, so the league never goes back to match play');
    test.setTimeout(480_000);
    const shot = journeyShots('league-public/undone');
    const { record } = recorder('undone', 'league-public/undone');
    const teams = teamsOf('V');
    const { ctx, op } = await operatorPage(browser, baseURL);
    let id;
    let waiting;

    try {
      await test.step('the operator plays the ring, accepts the shared ranks and undoes them', async () => {
        id = await seedRingTie(op, { name: 'Undone League', court: 'D', teams });
        waiting = await leagueRanks(op);
        expect((await acceptSharedRanks(op)).ok).toBe(true);
        const answer = await undoSharedRanks(op);
        record({ step: 'admin', action: 'Undo shared ranks', status: answer.status, asked: answer.asked });
        expect(answer.ok).toBe(true);
        await expect.poll(async () => (await readBanner(op)).shown, { message: 'the banner asks the tie again' }).toBe(true);
      });

      await test.step('the phone: Standings, no Winner, no Awards tab', async () => {
        await openViewerLeague(page, id);
        const table = await readLeagueTable(page);
        const ranks = await leagueRanks(page);
        record({ step: 'public', action: 'League tab after the Undo', table, ranks, offersAwards: await awardsTabOffered(page) });
        await shot(page, 'league-tab-undone', { fullPage: true });
        expect(table.heading).toBe('Standings');
        expect(table.winner).toBe('');
        expect(ranks).toEqual(waiting);
        expect(await awardsTabOffered(page)).toBe(false);
        // Nor does the Overview name a winner.
        await openViewer(page, id);
        expect(await overviewWinner(page).count()).toBe(0);
        await shot(page, 'overview-undone', { fullPage: true });
      });

      await test.step('the TV board after the Undo: the refetch drops the finished state', async () => {
        const board = await readBoard(page, 'D');
        record({ step: 'display', action: 'board after the Undo', ...board });
        await shot(page, 'board-undone');
        expect(board.text).not.toMatch(/final standings|podium/i);
      });
    } finally {
      await ctx.close();
    }
  });
});
