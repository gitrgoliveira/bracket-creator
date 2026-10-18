// Kachinuki (winner stays on) in POOLS.
//
// Fixture: a team competition, kachinuki, team size 3, format "Pools +
// Knockout", five teams in pools of at most three, one qualifier per pool. The
// draw makes a pool of three and a pool of two; the knockout is the final
// between the two pool winners. Everything is seeded through the interface
// (wizard, roster paste, Generate draw, Start). The members of the teams of the
// three-team pool are named at the court through the lineup panel before their
// first encounter.
//
// The draw decides who meets whom and in which order, so the three-team pool
// is scripted by the ROLE a team plays, not by its name:
//   1  its first encounter: Shiro's team (A) beats B, a hikiwake retiring both
//      on the way, B's last fighter beaten: winner stays on, a tie removes both,
//      a team exhausted (Record bout takes the last fighter off and no pairing
//      follows).
//   2  one of A/B against the third team (C): ends LEVEL on individual victories
//      and points, both lineups used up on a double hikiwake. A pool encounter
//      may end in a draw, so it is recorded as a draw (T) with no winner
//      marked. (Kachinuki has no representative bout anywhere, in a pool or a
//      knockout, so the sheet offering none here says nothing about pools.)
//   3  the remaining pair: a clean win (End match straight after the win).
// Encounter 3's winner is picked so the pool has a strict order on W, L and T
// alone (no tie-break flow). The two-team pool's only encounter is one bout won
// by Shiro's team, ended with End match. Expected standings are tallied from the
// scripted bouts, never read back from the app.
//
// Asserted: the pairing the winner-stays-on rule brings up after each bout, the
// recorded result of each encounter, the pool tables' eight figures and rank
// badges for every team on the admin Pools tab and on a spectator's phone, and
// the two pool winners seated in the final.
import { test, expect } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { completedRowFor, openShiaijo, sides, upNextCard } from '../fixtures/shiaijo.mjs';
import { EXPECT_TIMEOUT_MS, PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { FIGURES, openAdminPools, openViewerPools, poolBlocks, poolTeams, rowFigures } from '../fixtures/pools.mjs';
import {
  awardBoutIppon, boutNames, closeLineupPanel, currentBout, daihyosenControls, daihyosenOffer, doneRows, endMatch,
  endMatchButton, noPairingLine, openLineupFromUpNext, recordBout, recordBoutButton, startUpNextKachinuki, tieButton,
  typeLineup, BOUT_CHANGE,
} from '../fixtures/kachinuki.mjs';
import { tapOnSide } from '../fixtures/pace.mjs';

const JOURNEY = 'kachinuki-pools';

const TEAMS = [
  ['Team Hayabusa', 'Kita Dojo'], ['Team Kawasemi', 'Minami Dojo'], ['Team Tsubame', 'Higashi Dojo'],
  ['Team Washi', 'Nishi Dojo'], ['Team Taka', 'Chuo Dojo'],
];
const NAMES = TEAMS.map(([name]) => name);
// Senpo to Taisho.
const MEMBERS = {
  'Team Hayabusa': ['Aoki', 'Baba', 'Chiba'],
  'Team Kawasemi': ['Fujii', 'Goto', 'Hara'],
  'Team Tsubame': ['Kudo', 'Maeda', 'Noda'],
  'Team Washi': ['Taki', 'Ueno', 'Wada'],
  'Team Taka': ['Yano', 'Zaitsu', 'Ando'],
};
const TEAM_SIZE = 3;
const ORDINAL = ['1st', '2nd', '3rd'];

const zero = () => ({ W: 0, L: 0, T: 0, IV: 0, IL: 0, IT: 0, PW: 0, PL: 0 });

test.describe.configure({ mode: 'serial' });

test.describe('kachinuki in pools', () => {
  let shot;
  let compId;
  const audit = auditLog(JOURNEY);
  const { record } = audit;

  // What the scripted bouts add up to, per team (the pool tables' figures).
  const tally = Object.fromEntries(NAMES.map((n) => [n, zero()]));
  // The pools as the draw made them (team names), the three-team one first.
  let pools;
  // Teams whose members have been named at the court (a lineup carries to the
  // team's later matches).
  const lineupDone = new Set();

  test.beforeAll(() => {
    shot = journeyShots(JOURNEY);
  });

  test.afterAll(() => audit.flush());

  // Name the members of every team on the Up next card that has none yet.
  async function ensureLineups(page, pair) {
    const needing = [pair.shiro, pair.aka].filter((t) => !lineupDone.has(t));
    if (!needing.length) return;
    await openLineupFromUpNext(page);
    for (const team of needing) {
      await typeLineup(page, team, MEMBERS[team]);
      lineupDone.add(team);
    }
    await shot(page, `lineups-${needing.length}-teams-named`);
    await closeLineupPanel(page);
  }

  // Play `bouts` on the running sheet. Each entry is the winning team's name or
  // null for a hikiwake. The winner-stays-on pairing is tracked here and checked
  // against the sheet before every bout. A bout is scored M then K (2 points),
  // and recorded with Record bout unless it is the last one and `recordLast` is
  // false. Returns the encounter's per-team IV and PW for the Completed row.
  async function playBouts(page, pair, bouts, { recordLast, label }) {
    const next = { [pair.shiro]: 0, [pair.aka]: 0 };
    const other = (team) => (team === pair.shiro ? pair.aka : pair.shiro);
    const line = { [pair.shiro]: { IV: 0, PW: 0 }, [pair.aka]: { IV: 0, PW: 0 } };
    for (let n = 0; n < bouts.length; n += 1) {
      const winner = bouts[n];
      const last = n === bouts.length - 1;
      await expect.poll(() => boutNames(currentBout(page)), { message: `${label}: bout ${n + 1} pairing` }).toEqual({
        shiro: MEMBERS[pair.shiro][next[pair.shiro]], aka: MEMBERS[pair.aka][next[pair.aka]],
      });
      expect(await doneRows(page).count(), `${label}: fought bouts above the live one`).toBe(n);
      if (winner) {
        const side = winner === pair.shiro ? 'shiro' : 'aka';
        await awardBoutIppon(page, side, 'M');
        await awardBoutIppon(page, side, 'K');
        const loser = other(winner);
        tally[winner].IV += 1;
        tally[winner].PW += 2;
        tally[loser].IL += 1;
        tally[loser].PL += 2;
        line[winner].IV += 1;
        line[winner].PW += 2;
        next[loser] += 1;
      } else {
        const tie = tieButton(currentBout(page));
        await tie.tap();
        await expect(tie).toHaveText(/✓ Tie/);
        tally[pair.shiro].IT += 1;
        tally[pair.aka].IT += 1;
        next[pair.shiro] += 1;
        next[pair.aka] += 1;
      }
      await shot(page, `${label}-bout-${n + 1}-scored`);
      if (!last || recordLast) await recordBout(page);
    }
    return line;
  }

  // The Completed row's result string for a finished encounter, by side.
  const resultText = (pair, line) => new RegExp(
    `IV ${line[pair.shiro].IV}–${line[pair.aka].IV}\\s+PW ${line[pair.shiro].PW}–${line[pair.aka].PW}`);

  async function expectCompleted(page, pair, line) {
    const row = completedRowFor(page, pair);
    await expect(row).toBeVisible();
    await expect(row.locator('.shiaijo-qrow__result')).toHaveText(resultText(pair, line));
    return (await row.innerText()).replace(/\s+/g, ' ');
  }

  // The ranking the scripted results give within one pool: W high, L low, T high.
  const rankedIn = (teams) => [...teams].sort((a, b) => (tally[b].W - tally[a].W) || (tally[a].L - tally[b].L)
    || (tally[b].T - tally[a].T));

  // Roles of the three-team pool, fixed by its first two encounters.
  const role = {};

  // Encounter 1: winner stays on, a hikiwake retires both, a team is exhausted.
  async function encounter1(page, pair) {
    role.A = pair.shiro;
    role.B = pair.aka;
    await ensureLineups(page, pair);
    await startUpNextKachinuki(page);
    await shot(page, 'enc1-started');
    // A beats B's first, A stays on and ties B's second (both retire), A's
    // second beats B's third: B has nobody left.
    const line = await playBouts(page, pair, [role.A, null, role.A], { recordLast: true, label: 'enc1' });
    // Record bout took B's last fighter off: no pairing follows, and the sheet says so.
    await expect(noPairingLine(page)).toContainText('has no fighters left');
    await expect(recordBoutButton(page)).toBeDisabled();
    await expect(daihyosenOffer(page)).toHaveCount(0);
    await shot(page, 'enc1-b-exhausted');
    await endMatch(page);
    const rowText = await expectCompleted(page, pair, line);
    tally[role.A].W += 1;
    tally[role.B].L += 1;
    record({ journey: JOURNEY, step: 'enc1', action: 'End match', pair, rowText, screenshot: await shot(page, 'enc1-completed') });
  }

  // Encounter 2: level on victories and points, so a draw (T) with no winner marked.
  async function encounter2(page, pair) {
    role.C = pools[0].find((n) => n !== role.A && n !== role.B);
    const p = pair.shiro === role.C ? pair.aka : pair.shiro;
    expect([role.A, role.B], 'one of the first encounter\'s teams meets the third').toContain(p);
    await ensureLineups(page, pair);
    await startUpNextKachinuki(page);
    await shot(page, 'enc2-started');
    // P beats C's first; C's second beats P's first; P's second ties C's second
    // (both retire); P's third ties C's third. Both lineups are used up: the last
    // bout stays the current bout, Record bout is off, and the encounter is level
    // at 1 victory and 2 points each.
    const line = await playBouts(page, pair, [p, role.C, null, null], { recordLast: false, label: 'enc2' });
    // Record bout records the tie and finds nobody to bring up: the bout stays the
    // current one and Record bout is greyed out (team-tournaments.md, "A tie
    // leaves both teams with nobody").
    await expect(recordBoutButton(page)).toBeEnabled();
    // Paced against the Record bout of bout 3: the two share one bounce guard.
    await tapOnSide(page, BOUT_CHANGE, recordBoutButton(page));
    await expect(recordBoutButton(page)).toBeDisabled();
    expect(await doneRows(page).count(), 'the tied last bout stays the current bout').toBe(3);
    // Kachinuki has no representative bout in any round, so none is offered on
    // this tie. That holds in a knockout too; it is not a claim about pools.
    await expect(daihyosenOffer(page)).toHaveCount(0);
    await expect(daihyosenControls(page)).toHaveCount(0);
    await expect(endMatchButton(page)).toBeEnabled();
    await shot(page, 'enc2-level-no-daihyosen');
    await endMatch(page);
    const rowText = await expectCompleted(page, pair, line);
    // A draw names no winner.
    expect(rowText.match(/✓/g) || [], `a drawn encounter marks no winner: ${rowText}`).toHaveLength(0);
    tally[p].T += 1;
    tally[role.C].T += 1;
    record({ journey: JOURNEY, step: 'enc2', action: 'End match', pair, rowText, screenshot: await shot(page, 'enc2-completed') });
  }

  // Encounter 3: the remaining pair, a clean win ended with End match.
  async function encounter3(page, pair) {
    const [x, y] = [pair.shiro, pair.aka];
    // The winner that leaves the pool with a strict order on W, L, T.
    const order = (a, b) => (b.W - a.W) || (a.L - b.L) || (b.T - a.T);
    const strict = (winner) => {
      const after = Object.fromEntries(pools[0].map((n) => [n, { ...tally[n] }]));
      after[winner].W += 1;
      after[winner === x ? y : x].L += 1;
      const sorted = [...pools[0]].sort((a, b) => order(after[a], after[b]));
      return sorted.every((n, i) => i === 0 || order(after[sorted[i - 1]], after[n]) !== 0);
    };
    const winner = [x, y].find(strict);
    expect(winner, 'one winner leaves a strict order').toBeTruthy();
    await ensureLineups(page, pair);
    await startUpNextKachinuki(page);
    // Three wins in a row: the winner stays on against all three. The last win
    // is not recorded; End match finishes on the last scored bout.
    const line = await playBouts(page, pair, [winner, winner, winner], { recordLast: false, label: 'enc3' });
    await shot(page, 'enc3-last-bout-scored');
    await endMatch(page);
    const rowText = await expectCompleted(page, pair, line);
    tally[winner].W += 1;
    tally[winner === x ? y : x].L += 1;
    record({ journey: JOURNEY, step: 'enc3', action: 'End match', pair, winner, rowText, screenshot: await shot(page, 'enc3-completed') });
  }

  // The two-team pool's only encounter: one bout, Shiro's team wins it.
  async function quickEncounter(page, pair) {
    await startUpNextKachinuki(page);
    const line = { [pair.shiro]: { IV: 1, PW: 2 }, [pair.aka]: { IV: 0, PW: 0 } };
    await awardBoutIppon(page, 'shiro', 'M');
    await awardBoutIppon(page, 'shiro', 'K');
    await endMatch(page);
    const rowText = await expectCompleted(page, pair, line);
    Object.assign(tally[pair.shiro], { W: 1, IV: 1, PW: 2 });
    Object.assign(tally[pair.aka], { L: 1, IL: 1, PL: 2 });
    record({ journey: JOURNEY, step: 'pair pool', action: 'End match', pair, rowText, screenshot: await shot(page, 'pair-pool-completed') });
  }

  test('seed: five teams, a pool of three and a pool of two, one qualifier each', async ({ page }) => {
    await createTournament(page, { name: 'Kachinuki Pools Cup', courts: 2 });
    compId = await createCompetition(page, {
      name: 'Team Kachinuki Pools', kind: 'team', format: 'mixed', teamSize: TEAM_SIZE, teamMatchType: 'kachinuki',
      poolSizeMode: 'maximum', poolSize: 3, poolWinners: 1, courts: ['A'],
    });
    await pasteRoster(page, compId, TEAMS);
    await generateDraw(page, compId);
    await startCompetition(page, compId);

    // The pools as the draw made them, read back.
    await openAdminPools(page, compId);
    await expect(poolBlocks(page)).toHaveCount(2);
    const read = [];
    for (let i = 0; i < 2; i += 1) {
      const names = await poolTeams(poolBlocks(page).nth(i));
      read.push(NAMES.filter((n) => names.some((t) => t.includes(n))));
    }
    pools = read.sort((a, b) => b.length - a.length);
    record({ journey: JOURNEY, step: 'draw', action: 'pool composition', pools, screenshot: await shot(page, 'pools-drawn') });
    expect(pools.map((t) => t.length), 'a pool of three and a pool of two').toEqual([3, 2]);
  });

  test('play the pools: winner stays on, hikiwake, exhaustion, a level draw', async ({ page }) => {
    await login(page);
    const poolOf = (team) => pools.findIndex((t) => t.includes(team));
    let threeTeamPlayed = 0;
    for (let n = 1; n <= 4; n += 1) {
      await openShiaijo(page, 'A');
      await expect(upNextCard(page)).toBeVisible();
      const pair = await sides(upNextCard(page));
      expect(poolOf(pair.shiro), 'an encounter is within one pool').toBe(poolOf(pair.aka));
      const isThree = poolOf(pair.shiro) === 0;
      const title = !isThree ? `encounter ${n}: the pool of two`
        : ['winner stays on, a hikiwake retires both, a team is exhausted', 'level on victories and points is a draw (T)',
          'a clean win ended with End match'][threeTeamPlayed];
      await test.step(`encounter ${n} (${pair.shiro} v ${pair.aka}): ${title}`, async () => {
        if (!isThree) await quickEncounter(page, pair);
        else await [encounter1, encounter2, encounter3][threeTeamPlayed](page, pair);
      });
      if (isThree) threeTeamPlayed += 1;
    }
    expect(threeTeamPlayed).toBe(3);
  });

  async function expectTables(page, where) {
    for (const team of pools) {
      for (const [i, name] of rankedIn(team).entries()) {
        const want = { ...tally[name], rank: ORDINAL[i], advancing: i < 1 };
        // The table reads the server's standings: wait for the last write to show.
        await expect.poll(() => rowFigures(page, name), { message: `${where}: ${name}` }).toEqual(want);
      }
    }
  }

  test('the pool standings: team and individual figures, on the admin Pools tab and a spectator\'s phone', async ({ page, browser, baseURL }) => {
    await login(page);
    await openAdminPools(page, compId);
    await expectTables(page, 'admin Pools tab');
    record({ journey: JOURNEY, step: 'standings', action: 'admin Pools tab', columns: FIGURES, tally,
      screenshot: await shot(page, 'admin-pools-standings', { fullPage: true }) });

    const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    try {
      const phone = await spectator.newPage();
      await openViewerPools(phone, compId);
      await expectTables(phone, 'public viewer Pools tab');
      await shot(phone, 'viewer-pools-standings', { fullPage: true });
    } finally {
      await spectator.close();
    }
  });

  test('the winner of each pool is seated in the final', async ({ page }) => {
    await login(page);
    const winners = pools.map((t) => rankedIn(t)[0]).sort();
    // One navigation; the poll then reads the console as it follows the data
    // (a goto per poll interval would re-load the page under every read).
    await openShiaijo(page, 'A');
    await expect.poll(async () => {
      const card = upNextCard(page);
      if (!(await card.count())) return null;
      const pair = await sides(card);
      return [pair.shiro, pair.aka].sort();
    }, { message: 'the final seats the pool winners', timeout: EXPECT_TIMEOUT_MS }).toEqual(winners);
    const text = (await upNextCard(page).innerText()).replace(/\s+/g, ' ');
    record({ journey: JOURNEY, step: 'knockout', action: 'Up next after the pools', winners, text,
      screenshot: await shot(page, 'final-seated') });
  });
});
