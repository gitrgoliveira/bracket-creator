// The SPECTATOR's league journey, on the PUBLIC project (a 390x844 phone, no
// sign-in): a competitor, a coach or the public reading a team league on the
// competition page (League tab, Overview, Awards) and on the venue's TV board
// (/display?court=X), while a tie waits to be settled and once the court has
// settled it.
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
//   2. N3: the Overview names no winner while the tie waits
//   3. the tie settled by the daihyosen on the court console: "Final
//      standings", the Winner and the podium on the phone, and the board reads
//      the shiaijo as finished. A tie at 1st or 2nd cannot be accepted as
//      shared: only a tie-breaker with a winner (or lots) settles it.
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
import { flushAudits, journeyRecorder as recorder } from '../fixtures/audit.mjs';
import { enterTournament } from '../fixtures/setup.mjs';
import { openTvBoard, openViewer, tvBoard } from '../fixtures/public.mjs';
import { awardIppon } from '../fixtures/scoring.mjs';
import { openShiaijo } from '../fixtures/shiaijo.mjs';
import {
  awardsTabOffered, leagueRanks, openLeague, openNumberedRow, openViewerAwards, openViewerLeague,
  overviewWinner, playEncounter, playTiebreakOnCourt, readBanner, readLeagueTable, readNumberedRows, readPodium,
  retiredControls, runTiebreaker, seedLeague,
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
  test.afterAll(() => flushAudits(test.info().file));

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

      await test.step('the phone: the Overview while the tie waits (recorded; N3 asserts it)', async () => {
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

  test('N3: the Overview names no winner while a tie at the podium waits', async ({ page, browser, baseURL }) => {
    test.setTimeout(480_000);
    const shot = journeyShots('league-public/winner-too-early');
    const { record } = recorder('winner-too-early', 'league-public/winner-too-early');
    const teams = teamsOf('N');
    const { ctx, op } = await operatorPage(browser, baseURL);
    try {
      const id = await seedRingTie(op, { name: 'Winner Too Early', court: 'C', teams });
      await openViewer(page, id);
      await expect(page.locator('.viewer__body')).toBeVisible();
      record({ step: 'public', action: 'Overview while the tie waits', winnerBadges: await overviewWinner(page).count(), banner: (await readBanner(op)).shown });
      await shot(page, 'overview-tie-waits', { fullPage: true });
      expect(await overviewWinner(page).count()).toBe(0);
    } finally {
      await ctx.close();
    }
  });

  test('the tie settled by the daihyosen on the court console: the phone reads Final standings and the Awards tab, the TV board reads the shiaijo as finished', async ({ page, browser, baseURL }) => {
    test.setTimeout(480_000);
    const shot = journeyShots('league-public/settled');
    const { record } = recorder('settled', 'league-public/settled');
    const teams = teamsOf('U');
    const [a, b, c] = teams.ring;
    // The order the daihyosen give: a beats b and c, b beats c.
    const order = [a, b, c];
    const place = new Map(order.map((t, i) => [t, i]));
    const { ctx, op } = await operatorPage(browser, baseURL);
    let id;

    try {
      await test.step('the operator plays the ring: nothing offers to share the places', async () => {
        id = await seedRingTie(op, { name: 'Settled League', court: 'B', teams });
        const banner = await readBanner(op);
        record({ step: 'admin', action: 'banner of the ring tie', banner, retired: await retiredControls(op) });
        expect(await retiredControls(op)).toEqual({ tiebreakTop: 0, sharedRanks: 0 });
        await runTiebreaker(op, teams.ring);
      });

      await test.step('the court console plays the three daihyosen', async () => {
        for (let i = 1; i <= 3; i += 1) {
          await openShiaijo(op, 'B');
          const played = await playTiebreakOnCourt(op, {
            among: teams.ring, shot, flow: `settled-dh-${i}`,
            decide: (p, pair) => awardIppon(p, place.get(pair.shiro) < place.get(pair.aka) ? 'shiro' : 'aka', 'M'),
          });
          record({ step: 'court', action: `daihyosen ${i}`, pair: played.pair, finishLabel: played.finishLabel, shots: played.shots });
        }
        await openLeague(op, id);
        await expect.poll(async () => (await readLeagueTable(op)).heading, { message: 'the league finished' }).toBe('Final standings');
        record({ step: 'admin', action: 'after the daihyosen', table: await readLeagueTable(op),
          screenshot: await shot(op, 'settled-desk-table', { fullPage: true }) });
      });

      await test.step('the phone: Final standings, the ring in the order the daihyosen gave', async () => {
        await openViewerLeague(page, id);
        const table = await readLeagueTable(page);
        const ranks = await leagueRanks(page);
        record({ step: 'public', action: 'League tab once the tie is settled', table, ranks });
        await shot(page, 'league-tab-settled', { fullPage: true });
        expect(table.heading).toBe('Final standings');
        expect(table.played).toBe(9);
        expect(table.winner).toBe(a);
        expect(ranks).toEqual({ [a]: 1, [b]: 2, [c]: 3, [teams.last]: 4 });
      });

      await test.step('the phone: the Awards tab is offered and reads the podium', async () => {
        await openViewerAwards(page, id);
        const podium = await readPodium(page);
        record({ step: 'public', action: 'Awards tab once the tie is settled', podium });
        await shot(page, 'settled-public-awards', { fullPage: true });
        expect(podium.places.slice(0, 3).map((p) => p.name)).toEqual(order);
        expect(podium.places.map((p) => p.name)).not.toContain(teams.last);
      });

      await test.step('the TV board: the shiaijo reads as finished', async () => {
        const board = await readBoard(page, 'B');
        record({ step: 'display', action: 'board once the tie is settled', ...board });
        await shot(page, 'board-settled');
        expect(board.allCompleted).toBe(true);
      });
    } finally {
      await ctx.close();
    }
  });
});
