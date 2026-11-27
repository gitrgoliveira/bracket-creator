// Two competitors swapping places in a draw before the start, on the operator's
// iPad, with a spectator's phone open on the published draw.
//
// The organiser opens the Draw review card on the Overview, goes to Pools in
// swap mode from its button, picks two competitors, reads what the swap does
// in the check panel, and makes it. The swap puts two dojo-mates in one pool,
// so the check says so and its button reads Swap anyway. The open phone shows
// the new pools without a reload. Back on the Overview the card finds the clash
// and suggests a swap, which undoes it. A knockout draw, still hidden, is
// swapped on the Bracket the same way, and its check says nothing about the
// public pages.
//
// Every tap is the operator's: the tournament is seeded through the interface,
// and nothing is written over the API.
//
// Two individual competitions:
//   S  Swap Open      6 competitors, two pools of 3, court A, its draw public
//   K  Swap Knockout  4 competitors, knockout only, court B, its draw hidden
// Each holds ONE pair of dojo-mates (Fudo), so a swap can only bring that pair
// closer or keep it apart.
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { createTournament } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, publishDraw } from '../fixtures/competition.mjs';
import { bracketSlots, poolMembers, toastText } from '../fixtures/roster.mjs';
import { competitionTab, openViewer } from '../fixtures/public.mjs';
import { dwell, settled } from '../fixtures/pace.mjs';

const FUDO = ['Aiko Fujita', 'Kenji Fukuda'];
const POOLS_ROSTER = [
  [FUDO[0], 'Fudo'], [FUDO[1], 'Fudo'], ['Mei Hara', 'Hizen'],
  ['Noah Price', 'Thames'], ['Ola Berg', 'Musashi'], ['Ravi Shah', 'Mumeishi'],
];
const KO_FUDO = ['Sora Fujii', 'Taro Funaki'];
const KO_ROSTER = [[KO_FUDO[0], 'Fudo'], [KO_FUDO[1], 'Fudo'], ['Uma Grey', 'Hizen'], ['Vic Lund', 'Thames']];

// What the swap check says (web-mobile/js/admin_draw_swap_check.jsx).
const SHARE_POOL = 'Dojo-mates from Fudo would share a pool.';
const MEET_EARLIER = 'Dojo-mates from Fudo could meet earlier.';
const PUBLIC_LINE = 'The public pages update at once.';

const card = (op) => op.getByTestId('draw-review');
const banner = (op) => op.getByTestId('swap-mode');
const check = (op) => op.getByTestId('draw-swap-check');
const poolPick = (op, name) => op.locator('.pool [data-testid="swap-target"]').filter({ hasText: name });
const bracketPick = (op, name) => op.locator('.bc-side.swap-pick').filter({ hasText: name });

// The pool (its name) holding `name`.
const poolOf = (pools, name) => Object.keys(pools).find((p) => pools[p].includes(name));

// Tap a competitor in swap mode. Every pick shares one tap guard, so a second
// pick waits out the bounce window of the first.
async function pick(op, target, since) {
  if (since) await dwell(op, since);
  await target.tap();
  return Date.now();
}

// The check panel's answer arrived: Swap them (or Swap anyway) is enabled.
// Returns the button, after the panel's own bounce window.
async function checkAnswered(op) {
  await expect(check(op)).toBeVisible();
  const swapThem = check(op).getByTestId('swap-them');
  await expect(swapThem).toBeEnabled();
  await settled(swapThem);
  return swapThem;
}

test.describe.configure({ mode: 'serial' });

test.describe('two competitors swap places in a draw before the start', () => {
  let shot;
  let opCtx;
  let op; // the operator's iPad, signed in, kept for the whole describe
  let pubCtx;
  let phone; // the spectator's phone, open on the published draw from the first test on
  let poolsId;
  let koId;

  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    shot = journeyShots('draw-swap');
    const server = await serverForFile(testInfo.file);
    opCtx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: server.base });
    op = await opCtx.newPage();
    pubCtx = await browser.newContext({ ...PUBLIC_DEVICE, baseURL: server.base });
    phone = await pubCtx.newPage();
    await createTournament(op, { name: 'Swap Cup', courts: 2 });
    poolsId = await createCompetition(op, {
      name: 'Swap Open', kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A'], numberPrefix: 'S',
    });
    await pasteRoster(op, poolsId, POOLS_ROSTER);
    await generateDraw(op, poolsId);
    await publishDraw(op, poolsId);
    koId = await createCompetition(op, {
      name: 'Swap Knockout', kind: 'individual', format: 'knockout', courts: ['B'], numberPrefix: 'K',
    });
    await pasteRoster(op, koId, KO_ROSTER);
    await generateDraw(op, koId);
  });

  test.afterAll(async () => {
    await opCtx?.close();
    await pubCtx?.close();
  });

  test('a swap made by hand on Pools: picked, checked, made, and the open phone follows', async () => {
    let before;
    await test.step('the draw keeps the Fudo pair in separate pools', async () => {
      await op.goto(`/admin/competition/${poolsId}/pools`);
      before = await poolMembers(op);
      expect(Object.keys(before)).toHaveLength(2);
      expect(poolOf(before, FUDO[0])).not.toBe(poolOf(before, FUDO[1]));
    });

    await test.step('the phone shows the same pools', async () => {
      await openViewer(phone, poolsId);
      await competitionTab(phone, 'Pools').tap();
      await expect.poll(() => poolMembers(phone)).toEqual(before);
    });

    await test.step('the Draw review card lists the pair and opens Pools in swap mode', async () => {
      await op.goto(`/admin/competition/${poolsId}/overview`);
      await expect(card(op).getByTestId('dojo-separation-row')).toHaveCount(1);
      await expect(card(op).getByTestId('dojo-separation-row')).toContainText(FUDO[0]);
      await card(op).getByText('Looking for swaps', { exact: false }).waitFor({ state: 'detached' });
      await shot(op, 'review-before');
      await card(op).getByTestId('draw-review-swap-mode').tap();
      await expect(op).toHaveURL(new RegExp(`/admin/competition/${poolsId}/pools$`));
      await expect(banner(op)).toBeVisible();
      await expect(banner(op)).toBeFocused();
    });

    // Bring Aiko into Kenji's pool, trading places with one of Kenji's
    // pool-mates: the Fudo pair then shares a pool.
    const mover = FUDO[0];
    const partner = before[poolOf(before, FUDO[1])].find((n) => n !== FUDO[1]);
    let picked;
    await test.step(`pick ${mover}: the banner names the pick, and ${partner} is marked worse`, async () => {
      picked = await pick(op, poolPick(op, mover));
      await expect(poolPick(op, mover)).toHaveAttribute('aria-pressed', 'true');
      await expect(op.getByTestId('swap-mode-line')).toContainText(mover);
      await expect(poolPick(op, partner)).toHaveClass(/swap-pick--worse/);
      await expect(poolPick(op, partner).getByTestId('swap-verdict-word')).toHaveText('Worse for Fudo');
      await shot(op, 'pools-picked');
    });

    await test.step(`pick ${partner}: the check says the pair would share a pool, and the public pages update`, async () => {
      await pick(op, poolPick(op, partner), picked);
      const swapThem = await checkAnswered(op);
      await expect(check(op).getByTestId('swap-verdict')).toHaveText(SHARE_POOL);
      await expect(check(op)).toContainText(PUBLIC_LINE);
      await expect(swapThem).toHaveText('Swap anyway');
      await shot(op, 'pools-check');
      await swapThem.tap();
      await expect(check(op)).toHaveCount(0);
      await expect(op.getByTestId('swap-mode-line')).toHaveText(`Swapped ${mover} and ${partner}.`);
    });

    await test.step('the pools show the trade, on the iPad and on the open phone without a reload', async () => {
      const moved = (pools) => poolOf(pools, mover) === poolOf(before, FUDO[1]) && poolOf(pools, partner) === poolOf(before, mover);
      await expect.poll(async () => moved(await poolMembers(op))).toBe(true);
      const after = await poolMembers(op);
      await expect.poll(() => poolMembers(phone)).toEqual(after);
      await shot(op, 'pools-swapped');
      await shot(phone, 'phone-swapped');
    });

    await test.step('Done turns swap mode off', async () => {
      await dwell(op, Date.now());
      await banner(op).getByTestId('swap-mode-done').tap();
      await expect(banner(op)).toHaveCount(0);
      await expect(op.getByTestId('swap-mode-on')).toBeVisible();
    });
  });

  test('the Draw review card finds the clash, and its suggested swap undoes it', async () => {
    await op.goto(`/admin/competition/${poolsId}/overview`);
    await expect(card(op).getByTestId('draw-review-verdict')).toContainText('They share Pool');

    let swapThem;
    await test.step('the card suggests a swap, and its check says the pair could meet later', async () => {
      await expect(card(op).getByTestId('draw-review-conclusion')).toHaveText(/swaps? would keep dojo-mates apart longer/);
      const suggestion = card(op).getByTestId('swap-suggestion').first();
      await expect(suggestion).toBeVisible();
      await shot(op, 'review-clash');
      await settled(suggestion.getByRole('button', { name: /^Swap / }));
      await suggestion.getByRole('button', { name: /^Swap / }).tap();
      swapThem = await checkAnswered(op);
      await expect(check(op).getByTestId('swap-verdict')).toHaveText(/^Dojo-mates from Fudo could meet later\./);
      await expect(swapThem).toHaveText('Swap them');
      await shot(op, 'review-check');
    });

    await test.step('Swap them: a toast names the pair, and focus lands on the conclusion line', async () => {
      await swapThem.tap();
      expect(await toastText(op, /^Swapped /)).toMatch(/^Swapped .+ and .+\.$/);
      await expect(check(op)).toHaveCount(0);
      await expect(card(op).getByTestId('draw-review-conclusion')).toBeFocused();
      await expect(card(op).getByTestId('draw-review-verdict')).not.toContainText('They share Pool');
    });

    await test.step('the open phone shows the Fudo pair apart again', async () => {
      await expect.poll(async () => {
        const pools = await poolMembers(phone);
        return poolOf(pools, FUDO[0]) !== poolOf(pools, FUDO[1]);
      }).toBe(true);
      await shot(phone, 'phone-apart');
    });
  });

  test('a hidden knockout draw is swapped on the Bracket, and its check says nothing of the public pages', async () => {
    const firstRound = (slots) => Object.entries(slots).filter(([k]) => k.startsWith('m-r1-'));
    const matchOf = (slots, name) => firstRound(slots).find(([, s]) => s.name === name)?.[0].split(':')[0];
    let before;
    await test.step('the draw puts the Fudo pair in different first-round matches', async () => {
      await op.goto(`/admin/competition/${koId}/bracket`);
      before = await bracketSlots(op, (s) => firstRound(s).length === 4);
      expect(firstRound(before)).toHaveLength(4);
      expect(matchOf(before, KO_FUDO[0])).not.toBe(matchOf(before, KO_FUDO[1]));
    });

    // Sora takes the place of Taro's first-round opponent: the pair then
    // meets in the first round.
    const mover = KO_FUDO[0];
    let opponent;
    await test.step(`swap ${mover} with ${KO_FUDO[1]}'s opponent: the check says the pair could meet earlier`, async () => {
      const taroMatch = matchOf(before, KO_FUDO[1]);
      opponent = firstRound(before).find(([k, s]) => k.startsWith(`${taroMatch}:`) && s.name !== KO_FUDO[1])[1].name;
      await dwell(op, Date.now());
      await op.getByTestId('swap-mode-on').tap();
      await expect(banner(op)).toBeVisible();
      const picked = await pick(op, bracketPick(op, mover), Date.now());
      await expect(bracketPick(op, mover)).toHaveAttribute('aria-pressed', 'true');
      await pick(op, bracketPick(op, opponent), picked);
      const swapThem = await checkAnswered(op);
      await expect(check(op).getByTestId('swap-verdict')).toHaveText(MEET_EARLIER);
      await expect(check(op)).not.toContainText(PUBLIC_LINE);
      await expect(swapThem).toHaveText('Swap anyway');
      await shot(op, 'bracket-check');
      await swapThem.tap();
      await expect(op.getByTestId('swap-mode-line')).toHaveText(`Swapped ${mover} and ${opponent}.`);
    });

    await test.step('the bracket shows the pair in one first-round match', async () => {
      const after = await bracketSlots(op, (s) => matchOf(s, KO_FUDO[0]) && matchOf(s, KO_FUDO[0]) === matchOf(s, KO_FUDO[1]));
      expect(matchOf(after, KO_FUDO[0])).toBe(matchOf(after, KO_FUDO[1]));
      expect(matchOf(after, opponent)).toBe(matchOf(before, KO_FUDO[0]));
      await shot(op, 'bracket-swapped');
    });
  });
});
