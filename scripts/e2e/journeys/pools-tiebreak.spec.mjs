// Pools + knockout with a MINIMUM pool size, and a pool tie that needs a
// tie-break bout (bc-e2tb).
//
// Fixture: an individual "Pools + Knockout" competition, seven entrants, pool
// size a MINIMUM of 3, two qualifiers per pool, one shiaijo. The minimum rule
// divides the entrant count down (docs/user-guide/organisers/pool-draw.md:
// "every pool reaches the minimum and no extra pool opens for the remainder; the
// players left over join existing pools, one each"), so seven entrants make two
// pools, of 4 and of 3. A MAXIMUM of 3 would open three pools (3, 2, 2): seven
// is the smallest roster on which the two modes differ.
//
// The draw decides who is in which pool and who meets whom, so the bouts are
// scripted by the ROLE a competitor plays (their place in the pool's draw
// order), never by name, and a bout's script does not depend on which side the
// draw gives it:
//   pool of 3, players A B C   A beats B and C 1-0 (A is first, W2).
//                              B and C fight a 1-1 hikiwake. B and C end level on
//                              EVERY criterion (W0 L1 D1, PW1, PL2) for the 2nd
//                              place, which advances: the engine adds a
//                              tie-break bout between them (internal/engine/
//                              tiebreaker.go, id "Pool X-TB-N").
//   pool of 4, players P Q R S P beats Q, R and S (R by two points), Q and R beat
//                              S, and Q and R draw (Mark draw, no points). Q and R
//                              end level on W1 L1 D1 and PW1 and differ only on
//                              points lost (Q 1, R 2): the LATER criterion
//                              orders them, Q above R, so NO tie-break bout is
//                              added (CLAUDE.md, Key Algorithms: wins, losses,
//                              hikiwake, points scored, points lost).
// Expected figures and orders are tallied from these scripts, never read back
// from the app.
//
// Asserted: the pool sizes the minimum rule makes; the tie-break bout appearing
// on the court console and the Scores tab as the only bout left to score, with
// no "Match n of m" label, while the pool table shows B and C level on every
// figure and marked tied; no tie-break bout for the pool settled on points
// lost; after the tie-break bout, the winner ranked above the loser and seated
// in the knockout (the loser out), on the admin pages and the public viewer.
import { test, expect } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { completedRowFor, openShiaijo, sides, upNextCard } from '../fixtures/shiaijo.mjs';
import { EXPECT_TIMEOUT_MS, PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { INDIVIDUAL_FIGURES, openAdminPools, openViewerPools, playerRowFigures, poolBlocks, poolTeams } from '../fixtures/pools.mjs';
import { ROSTER_7, editorSides, ensureRunning, inlineRunning, playScriptedBout } from '../fixtures/individual.mjs';
import { awardIppon, finishMatch, nextMatchWrite, writeLanded } from '../fixtures/scoring.mjs';
import { scoreRows } from '../fixtures/scores.mjs';
import { bracketSlots, openCompetitionTab } from '../fixtures/roster.mjs';
import { openViewer } from '../fixtures/public.mjs';

const JOURNEY = 'pools-tiebreak';
const MIN_POOL = 3;
const POOL_WINNERS = 2;
const NAMES = ROSTER_7.map(([name]) => name);
const ORDINAL = ['1st', '2nd', '3rd', '4th'];
// A pool bout reads "Match n of m"; a tie-break bout is the bare pairing.
const POOL_MATCH_LABEL = /Match \d+ of \d+/i;

// The pool sizes the minimum rule makes: floor(n / min) pools, the remainder
// joining existing pools one each.
function minimumRuleSizes(entrants, min) {
  const pools = Math.floor(entrants / min);
  const leftover = entrants - pools * min;
  return Array.from({ length: pools }, (_, i) => min + (i < leftover ? 1 : 0)).sort((a, b) => b - a);
}

const zero = () => ({ W: 0, L: 0, D: 0, PW: 0, PL: 0 });
// The pool ranking: wins, then fewer losses, then more hikiwake, then more
// points scored, then fewer points lost. 0 means level on every criterion.
const order = (a, b) => (b.W - a.W) || (a.L - b.L) || (b.D - a.D) || (b.PW - a.PW) || (a.PL - b.PL);

test.describe.configure({ mode: 'serial' });

test.describe('pools: minimum pool size and a tie-break bout', () => {
  let shot;
  let compId;
  const audit = auditLog(JOURNEY);
  const { record } = audit;

  // Draw order of each pool as the draw made it, the pool of 4 first.
  let big;
  let small;
  // The scripted bouts: sorted-pair key -> { marks: { name: [waza] } } | { draw }.
  const plan = new Map();
  const tally = Object.fromEntries(NAMES.map((n) => [n, zero()]));
  // Set once the tie-break bout is on the console.
  let tb;
  // Who won the tie-break bout, who lost it.
  let tbWinner;
  let tbLoser;
  // The rank badge each of the level pair carried before the bout.
  let rankBefore;

  test.beforeAll(() => {
    shot = journeyShots(JOURNEY);
  });

  test.afterAll(() => audit.flush());

  const key = (a, b) => [a, b].sort().join(' | ');

  // Script one bout and add it to the expected tally.
  function script(a, b, entry) {
    plan.set(key(a, b), entry);
    if (entry.draw) {
      tally[a].D += 1;
      tally[b].D += 1;
      return;
    }
    const pa = (entry.marks[a] || []).length;
    const pb = (entry.marks[b] || []).length;
    tally[a].PW += pa;
    tally[a].PL += pb;
    tally[b].PW += pb;
    tally[b].PL += pa;
    if (pa === pb) {
      tally[a].D += 1;
      tally[b].D += 1;
    } else {
      const [win, lose] = pa > pb ? [a, b] : [b, a];
      tally[win].W += 1;
      tally[lose].L += 1;
    }
  }

  const planFor = ({ shiro, aka }) => {
    const entry = plan.get(key(shiro, aka));
    if (!entry) throw new Error(`no scripted result for ${shiro} v ${aka}`);
    return entry.draw ? { draw: true } : { shiro: entry.marks[shiro] || [], aka: entry.marks[aka] || [] };
  };

  // The competitor the tie-break bout is between: the pairing on the console,
  // whether the bout waits as Up next or already runs in the editor.
  async function tieBreakPairing(page) {
    await openShiaijo(page, 'A');
    let pair = null;
    await expect.poll(async () => {
      pair = (await inlineRunning(page))
        ? await editorSides(page).catch(() => null)
        : await sides(upNextCard(page)).catch(() => null);
      return pair && [pair.shiro, pair.aka].sort();
    }, { message: 'the court holds the tie-break bout', timeout: EXPECT_TIMEOUT_MS }).toEqual([tb.b, tb.c].sort());
    return pair;
  }

  async function expectTable(page, where, names, want) {
    for (const name of names) {
      await expect.poll(() => playerRowFigures(page, name), { message: `${where}: ${name}` })
        .toMatchObject(want(name));
    }
  }

  test('seed: seven entrants, pool size a minimum of 3, the draw makes a pool of 4 and a pool of 3', async ({ page }) => {
    await createTournament(page, { name: 'Pools Tiebreak Cup', courts: 1 });
    compId = await createCompetition(page, {
      name: 'Minimum Pools', kind: 'individual', format: 'mixed', poolSizeMode: 'minimum', poolSize: MIN_POOL,
      poolWinners: POOL_WINNERS, courts: ['A'],
    });
    await pasteRoster(page, compId, ROSTER_7);
    await generateDraw(page, compId);
    await startCompetition(page, compId);

    await openAdminPools(page, compId);
    const want = minimumRuleSizes(NAMES.length, MIN_POOL);
    expect(want, 'the rule makes two pools, 4 and 3 (a maximum of 3 would make 3, 2 and 2)').toEqual([4, 3]);
    await expect(poolBlocks(page)).toHaveCount(want.length);
    const read = [];
    for (let i = 0; i < want.length; i += 1) {
      const names = await poolTeams(poolBlocks(page).nth(i));
      read.push(names.map((t) => NAMES.find((n) => t.includes(n))));
    }
    const pools = read.sort((a, b) => b.length - a.length);
    expect(pools.map((p) => p.length), 'the draw cut the pools the minimum rule describes').toEqual(want);
    expect(pools.flat().sort(), 'every entrant is in exactly one pool').toEqual([...NAMES].sort());
    [big, small] = pools;
    record({ journey: JOURNEY, step: 'draw', action: 'pool sizes', rule: `floor(7 / ${MIN_POOL}) pools, leftover joins one each`, pools,
      screenshot: await shot(page, 'pools-drawn') });

    // The scripts, by role (draw order within the pool).
    const [A, B, C] = small;
    const [P, Q, R, S] = big;
    script(A, B, { marks: { [A]: ['M'] } });
    script(A, C, { marks: { [A]: ['M'] } });
    script(B, C, { marks: { [B]: ['M'], [C]: ['M'] } });
    script(P, Q, { marks: { [P]: ['M'] } });
    script(P, R, { marks: { [P]: ['M', 'K'] } });
    script(P, S, { marks: { [P]: ['M'] } });
    script(Q, S, { marks: { [Q]: ['M'] } });
    script(R, S, { marks: { [R]: ['M'] } });
    script(Q, R, { draw: true });
    tb = { a: A, b: B, c: C };
    // Level on every criterion, by construction.
    expect(order(tally[B], tally[C]), 'B and C are level on every criterion').toBe(0);
    // Q and R are level on W, L, D, PW and apart on PL only.
    expect(['W', 'L', 'D', 'PW'].map((f) => tally[Q][f]), 'Q and R agree up to points scored')
      .toEqual(['W', 'L', 'D', 'PW'].map((f) => tally[R][f]));
    expect(tally[Q].PL < tally[R].PL, 'points lost alone orders Q above R').toBe(true);
  });

  test('play the nine regular bouts on the court console', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');
    for (let n = 1; n <= 9; n += 1) {
      const played = await test.step(`regular bout ${n}`, () => playScriptedBout(page, planFor));
      record({ journey: JOURNEY, step: `bout ${n}`, action: 'Finish', shiro: played.shiro, aka: played.aka, plan: played.plan });
    }
    await shot(page, 'regular-bouts-done');
  });

  test('the level pair: a tie-break bout is added, and the table shows them level until it is fought', async ({ page }) => {
    await login(page);
    const pair = await tieBreakPairing(page);
    record({ journey: JOURNEY, step: 'tie-break', action: 'on the court console', pair, screenshot: await shot(page, 'tie-break-on-console') });
    const card = (await inlineRunning(page)) ? page.locator('.editor-modal__eyebrow').first() : upNextCard(page);
    expect((await card.innerText()), 'a tie-break bout carries no "Match n of m"').not.toMatch(POOL_MATCH_LABEL);

    // The Scores tab: it is the one bout left to score.
    await openCompetitionTab(page, compId, 'scores');
    await expect(scoreRows(page).first()).toBeVisible();
    const toScore = scoreRows(page).filter({ has: page.getByRole('button', { name: /^Score$/ }) });
    await expect(toScore, 'the tie-break bout is the only bout left to score').toHaveCount(1);
    await expect(toScore.first()).toContainText(tb.b);
    await expect(toScore.first()).toContainText(tb.c);
    expect(await toScore.first().innerText()).not.toMatch(POOL_MATCH_LABEL);
    await shot(page, 'scores-tab-tie-break');

    // The pool table: B and C level on every figure and marked tied; A first.
    const level = (name) => ({ ...tally[name], tied: true });
    await openAdminPools(page, compId);
    await expectTable(page, 'admin Pools tab', [tb.b, tb.c], level);
    const rows = { b: await playerRowFigures(page, tb.b), c: await playerRowFigures(page, tb.c) };
    // The badges the level pair carries until the bout: the table lists them in
    // draw order and badges the one drawn first higher, since the figures say
    // nothing. The tie-break winner is picked from the one badged LOWER, so the
    // bout visibly changes the order.
    rankBefore = { [tb.b]: rows.b.rank, [tb.c]: rows.c.rank };
    record({ journey: JOURNEY, step: 'tie-break', action: 'table before the bout', columns: INDIVIDUAL_FIGURES, rows, screenshot: await shot(page, 'admin-pools-level', { fullPage: true }) });
    await expectTable(page, 'admin Pools tab', [tb.a], (n) => ({ ...tally[n], rank: '1st', advancing: true, tied: false }));

    // The pool of 4: Q above R on points lost alone, no tie, no bout for them.
    const [P, Q, R, S] = big;
    const ranked = [P, Q, R, S];
    expect([...big].sort((x, y) => order(tally[x], tally[y])), 'the tally orders P, Q, R, S').toEqual(ranked);
    await expectTable(page, 'admin Pools tab', big, (n) => ({
      ...tally[n], rank: ORDINAL[ranked.indexOf(n)], advancing: ranked.indexOf(n) < POOL_WINNERS, tied: false,
    }));
    await shot(page, 'admin-pools-points-lost-order', { fullPage: true });

    // The bracket: the tied place is not seated yet (it waits for the tie-break).
    await openCompetitionTab(page, compId, 'bracket');
    const seated = Object.values(await bracketSlots(page)).map((s) => s.name);
    record({ journey: JOURNEY, step: 'tie-break', action: 'bracket before the bout', seated, screenshot: await shot(page, 'admin-bracket-before') });
    expect(seated, 'the pool of 4 is settled: its two qualifiers are seated').toEqual(expect.arrayContaining([ranked[0], ranked[1]]));
    for (const name of [tb.b, tb.c]) {
      expect(seated, `${name} is not seated while the tie is open`).not.toContain(name);
    }
  });

  test('score the tie-break bout: the winner ranks above the loser', async ({ page }) => {
    await login(page);
    const pair = await tieBreakPairing(page);
    // The bout starts from its Up next card unless a Finish + Start Next already did.
    const running = await ensureRunning(page);
    expect(running, 'the tie-break bout runs on the court').toEqual(pair);
    // The one the table badged lower wins, one men (aka when it badged both alike).
    const rankIndex = (name) => ORDINAL.indexOf(rankBefore[name]);
    const side = rankIndex(pair.shiro) > rankIndex(pair.aka) ? 'shiro' : 'aka';
    tbWinner = pair[side];
    tbLoser = pair[side === 'shiro' ? 'aka' : 'shiro'];
    const wrote = nextMatchWrite(page);
    await awardIppon(page, side, 'M');
    const res = await writeLanded(wrote, 'the tie-break bout\'s first mark');
    const id = decodeURIComponent(new URL(res.url()).pathname.split('/matches/')[1].replace(/\/score$/, ''));
    expect(id, 'the bout is the pool\'s supplementary bout').toMatch(/^Pool [A-Z]-TB-\d+$/);
    await shot(page, 'tie-break-scored');
    await finishMatch(page);
    await expect(completedRowFor(page, pair)).toBeVisible();
    record({ journey: JOURNEY, step: 'tie-break', action: 'Finish', id, winner: tbWinner, loser: tbLoser,
      screenshot: await shot(page, 'tie-break-finished') });

    await openAdminPools(page, compId);
    await expectOrdered(page, 'admin Pools tab');
    await shot(page, 'admin-pools-after', { fullPage: true });
  });

  // After the tie-break bout: the pool of 3 is A, the winner, the loser; the
  // level pair is no longer marked tied; the pool of 4 is unchanged.
  async function expectOrdered(page, where) {
    const [A] = [tb.a];
    const first = [A, tbWinner, tbLoser];
    await expectTable(page, where, first, (n) => ({
      rank: ORDINAL[first.indexOf(n)], advancing: first.indexOf(n) < POOL_WINNERS, tied: false,
    }));
    const [P, Q, R, S] = big;
    const ranked = [P, Q, R, S];
    await expectTable(page, where, ranked, (n) => ({
      ...tally[n], rank: ORDINAL[ranked.indexOf(n)], advancing: ranked.indexOf(n) < POOL_WINNERS, tied: false,
    }));
    record({ journey: JOURNEY, step: 'tie-break', action: `table after the bout (${where})`,
      rows: Object.fromEntries(await Promise.all(first.map(async (n) => [n, await playerRowFigures(page, n)]))) });
  }

  test('the winner, not the loser, is seated in the knockout: admin and public viewer', async ({ page, browser, baseURL }) => {
    await login(page);
    const qualifiers = [big[0], big[1], tb.a, tbWinner].sort();
    const seatedIn = async (p) => Object.values(await bracketSlots(p, (slots) => Object.values(slots).length === 4))
      .map((s) => s.name).sort();
    await openCompetitionTab(page, compId, 'bracket');
    expect(await seatedIn(page), 'the admin bracket seats the four qualifiers').toEqual(qualifiers);
    await shot(page, 'admin-bracket-after');

    const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    try {
      const phone = await spectator.newPage();
      await openViewerPools(phone, compId);
      await expectOrdered(phone, 'public viewer Pools tab');
      await shot(phone, 'viewer-pools-after', { fullPage: true });
      await openViewer(phone, compId);
      await phone.getByRole('button', { name: 'Bracket' }).tap();
      expect(await seatedIn(phone), 'the public bracket seats the same four').toEqual(qualifiers);
      await shot(phone, 'viewer-bracket-after');
    } finally {
      await spectator.close();
    }
    expect(qualifiers, 'the tie-break loser is out').not.toContain(tbLoser);
    record({ journey: JOURNEY, step: 'knockout', action: 'seated', qualifiers, out: [tbLoser, big[2], big[3]] });
  });
});
