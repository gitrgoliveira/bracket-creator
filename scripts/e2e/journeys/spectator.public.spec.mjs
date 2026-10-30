// The spectator's journey, on the PUBLIC project (a 390x844 phone, no sign-in):
// the public home, a competition page and its tabs, the schedule, competitor
// search by number, name and dojo (the bc-nsrc rule), the watchlist, and the
// match card opened from every door while an operator scores.
//
// The test's own `page` is the phone. The tournament is seeded and scored from
// a SECOND context opened with the operator's iPad (OPERATOR_DEVICE), through
// the interface, so the tournament is OFFICIATED: a spectator reads, never
// scores (self-run public scoring is a different journey, mp-yqxn.5).
//
// Two individual competitions with number prefixes chosen so the search rule
// can be seen:
//   K  Kendo Open  12 competitors, K1..K12 (so K1 and K12 both exist), court A
//   M  Women Open   6 competitors, M1..M6, court B; no name or dojo of the M
//      draw holds a "k" or a digit, so "k" and "1" can only find a K competitor
//      by number, or nobody at all.
// Pools of 3 (two through), so every match has both sides at once. Court A
// plays Pool A first, three bouts among the same three competitors. The tests
// read each pairing off the operator's court console and each number off the
// public pages, and assume neither: only the prefixes and the counts are fixed.
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { dwell } from '../fixtures/pace.mjs';
import { createTournament } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { inlineEditor, openShiaijo, sides, startUpNext, upNextCard, waitingRows } from '../fixtures/shiaijo.mjs';
import {
  awardIppon, clearMarks, filledSlots, finishMatch, settled,
  ipponButton, held, notHeld, running,
} from '../fixtures/scoring.mjs';
import { editorSides } from '../fixtures/individual.mjs';
import {
  NATURAL, applyScheduleText, cardMarks, cardSlots, clearScheduleText, competitionTab, homeNowRow, expandedCard, matchCard, nowCard, numbersOf, pickerEntries,
  pickerOption, pickerPairs, poolRow, scheduleCount, scheduleFilterInput, scheduleRow, scheduleRows, upNextRow, watchHero,
  watchHeroMatch, watchInput,
} from '../fixtures/public.mjs';

// Twelve K competitors, six dojos plus one with a single member (Shudokan).
const KENDO = [
  ['Akira Tanaka', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Chie Mori', 'Musashi'], ['Dan Evans', 'Seishinkan'],
  ['Eri Kato', 'Gyokusen'], ['Finn Hale', 'Thames'], ['Goro Abe', 'Musashi'], ['Hana Ito', 'Seishinkan'],
  ['Ivo Nunes', 'Mumeishi'], ['Jun Oda', 'Hizen'], ['Lea Roth', 'Mumeishi'], ['Sora Endo', 'Shudokan'],
];
// Six M competitors: no "k" and no digit in any name or dojo.
const WOMEN = [
  ['Ana Rossi', 'Thames'], ['Bea Lopez', 'Musashi'], ['Cleo Dane', 'Hizen'],
  ['Dora Pine', 'Mumeishi'], ['Ella Quinn', 'Bristol'], ['Fay Stone', 'Nottingham'],
];

test.describe.configure({ mode: 'serial' });

test.describe('spectator on a phone', () => {
  const audit = auditLog('spectator');
  const { record } = audit;
  let shot;
  let opCtx;
  let op; // the operator's page, signed in, kept for the whole file
  const ids = {};

  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    shot = journeyShots('spectator');
    const server = await serverForFile(testInfo.file);
    opCtx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: server.base });
    op = await opCtx.newPage();
    await createTournament(op, { name: 'Spectator Cup', courts: 2 });
    ids.kendo = await createCompetition(op, {
      name: 'Kendo Open', kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A'], numberPrefix: 'K',
    });
    await pasteRoster(op, ids.kendo, KENDO);
    await generateDraw(op, ids.kendo);
    await startCompetition(op, ids.kendo);
    ids.women = await createCompetition(op, {
      name: 'Women Open', kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['B'], numberPrefix: 'M',
    });
    await pasteRoster(op, ids.women, WOMEN);
    await generateDraw(op, ids.women);
    await startCompetition(op, ids.women);
    // The bout court A plays first, read from the operator's console.
    await openShiaijo(op, 'A');
    ids.first = await sides(upNextCard(op));
  });

  test.afterAll(async () => {
    audit.flush();
    await opCtx?.close();
  });

  test('the public home, a competition page and its tabs, and the schedule show the competitions and matches', async ({ page }) => {
    await test.step('the home lists both competitions, the schedule link and what plays next', async () => {
      await page.goto('/');
      await expect(page.locator('.viewer__title')).toHaveText('Spectator Cup');
      const kendo = page.locator('.vlist-item--comp', { hasText: 'Kendo Open' });
      const women = page.locator('.vlist-item--comp', { hasText: 'Women Open' });
      await expect(kendo).toContainText('12 players');
      await expect(women).toContainText('6 players');
      await expect(page.locator('.viewer-nav-card')).toContainText('18 matches · 2 courts');
      // Up next: the first three bouts of court A, K numbers beside the names.
      await expect(page.locator('.viewer__upnext-title')).toHaveText(/^Up next · 3$/);
      await expect(page.locator('button.vsched-item')).toHaveCount(3);
      await expect(page.locator('button.vsched-item').first()).toContainText(ids.first.shiro);
      await expect(page.locator('button.vsched-item').first()).toContainText(ids.first.aka);
      await expect(page.locator('button.vsched-item').first()).toContainText(/K\d+/);
      await shot(page, 'home', { fullPage: true });
    });

    await test.step('a competition card opens its page: Overview, Pools and Bracket tabs', async () => {
      await page.locator('.vlist-item--comp', { hasText: 'Kendo Open' }).tap();
      await expect(page).toHaveURL(new RegExp(`/competition/${ids.kendo}$`));
      for (const tab of ['Overview', 'Pools', 'Bracket']) await expect(competitionTab(page, tab)).toBeVisible();
      // Overview: the next matches.
      await expect(page.locator('.section-title', { hasText: /^Up next/ })).toBeVisible();
      await expect(upNextRow(page, ids.first)).toBeVisible();
      await shot(page, 'competition-overview');

      // Pools: four pools of three, every match listed once.
      await competitionTab(page, 'Pools').tap();
      await expect(page.locator('.pool-match-numbered-row')).toHaveCount(12);
      await expect(page.getByText(/^Pool [A-D]$/)).toHaveCount(4);
      await expect(poolRow(page, ids.first)).toBeVisible();
      await shot(page, 'competition-pools');

      // Bracket: the knockout tree, drawn before the pools are played.
      await competitionTab(page, 'Bracket').tap();
      await expect(competitionTab(page, 'Bracket')).toHaveClass(/is-active/);
      await expect(page.getByText('Pool A-1st').first()).toBeVisible();
      await shot(page, 'competition-bracket');
    });

    await test.step('the second competition has its own page, with its pools', async () => {
      await page.goto(`/competition/${ids.women}`);
      await competitionTab(page, 'Pools').tap();
      await expect(page.locator('.pool-match-numbered-row')).toHaveCount(6);
      await expect(page.getByText(/^Pool [A-B]$/)).toHaveCount(2);
    });

    await test.step('the schedule lists every match on both courts', async () => {
      await page.goto('/');
      await page.locator('.viewer-nav-card').tap();
      await expect(page).toHaveURL(/\/schedule$/);
      await expect(scheduleRows(page)).toHaveCount(18);
      await expect(scheduleCount(page)).toHaveText('18 matches of 18');
      await expect(page.locator('.tw-court', { hasText: 'SHIAIJO A' }).locator('.tw-court__sub')).toHaveText('12 matches');
      await expect(page.locator('.tw-court', { hasText: 'SHIAIJO B' }).locator('.tw-court__sub')).toHaveText('6 matches');
      await expect(scheduleRow(page, ids.first)).toContainText('Kendo Open');
      await shot(page, 'schedule', { fullPage: true });
    });
  });

  test('competitor search: the number rule (bc-nsrc), names and dojos, on the watch picker and the schedule', async ({ page }) => {
    const KENDO_NUMBERS = numbersOf('K', 12);
    const WOMEN_NUMBERS = numbersOf('M', 6);
    // name -> number, learned from the lists themselves (the draw hands the
    // numbers out; only the prefixes and the counts are fixed by the seeding).
    const numberOf = {};
    const learn = async (page_) => { for (const p of await pickerPairs(page_)) numberOf[p.name] = p.number; };
    const numbersFor = (...names) => names.map((n) => numberOf[n]).sort(NATURAL);

    // One surface's expectations, run against both pickers: the same rule, so
    // the same answers. `type` puts a query in the box; `empty` checks the
    // surface's own words for "nobody".
    const checkRule = async (surface, type, empty) => {
      // The prefix on its own lists the whole draw, and no other prefix.
      await type('k');
      await expect.poll(() => pickerEntries(page), { message: `${surface}: "k" lists every K and no M` })
        .toMatchObject({ numbers: KENDO_NUMBERS });
      await learn(page);
      expect(Object.values(numberOf).filter((n) => n.startsWith('K')), `${surface}: twelve K competitors`).toHaveLength(12);
      expect((await pickerEntries(page)).numbers.filter((n) => !n.startsWith('K')), `${surface}: "k" lists no other prefix`).toEqual([]);
      await shot(page, `${surface}-k`);
      record({ journey: 'spectator', surface, query: 'k', ...(await pickerEntries(page)) });

      // The prefix and the digits find one competitor: K1 is not K12 (nor K10, K11).
      const k1 = Object.keys(numberOf).find((n) => numberOf[n] === 'K1');
      const k12 = Object.keys(numberOf).find((n) => numberOf[n] === 'K12');
      await type('k1');
      await expect.poll(() => pickerEntries(page), { message: `${surface}: "k1" lists K1 alone` })
        .toEqual({ numbers: ['K1'], names: [k1] });
      await shot(page, `${surface}-k1`);
      record({ journey: 'spectator', surface, query: 'k1', ...(await pickerEntries(page)) });
      await type('K12');
      await expect.poll(() => pickerEntries(page), { message: `${surface}: "K12" lists K12 alone` })
        .toEqual({ numbers: ['K12'], names: [k12] });

      // The second prefix, the same way: the whole M draw is listed (with any K
      // competitor whose name or dojo holds an "m", as Musashi does), and M3 alone.
      await type('m');
      await expect.poll(async () => (await pickerEntries(page)).numbers, { message: `${surface}: "m" lists the whole M draw` })
        .toEqual(expect.arrayContaining(WOMEN_NUMBERS));
      await learn(page);
      await type('m3');
      await expect.poll(async () => (await pickerEntries(page)).numbers, { message: `${surface}: "m3" lists M3 alone` })
        .toEqual(['M3']);

      // Digits on their own are not a competitor number: nobody, there being no
      // digit in any name or dojo.
      await type('1');
      await expect.poll(() => pickerEntries(page), { message: `${surface}: "1" finds nobody` })
        .toEqual({ numbers: [], names: [] });
      await empty();
      await shot(page, `${surface}-1`);
      record({ journey: 'spectator', surface, query: '1', ...(await pickerEntries(page)) });

      // Names and dojos match on any part of the text.
      await type('tanak');
      await expect.poll(() => pickerEntries(page), { message: `${surface}: a name fragment` })
        .toEqual({ numbers: numbersFor('Akira Tanaka'), names: ['Akira Tanaka'] });
      await type('bristol');
      await expect.poll(() => pickerEntries(page), { message: `${surface}: a dojo fragment` })
        .toEqual({ numbers: numbersFor('Ella Quinn'), names: ['Ella Quinn'] });
      await type('thames');
      await expect.poll(() => pickerEntries(page), { message: `${surface}: a dojo shared by two draws` })
        .toEqual({ numbers: numbersFor('Ana Rossi', 'Ben Carter', 'Finn Hale'), names: ['Ana Rossi', 'Ben Carter', 'Finn Hale'] });
      record({ journey: 'spectator', surface, query: 'thames', ...(await pickerEntries(page)) });
    };

    await test.step('the home\'s watch picker', async () => {
      await page.goto('/');
      const box = watchInput(page);
      await box.tap();
      await checkRule('watch-picker', (q) => box.fill(q), () => expect(page.getByTestId('watchpicker-empty')).toContainText('No one here matches'));
    });

    await test.step('the schedule\'s filter picker', async () => {
      await page.goto('/schedule');
      const box = scheduleFilterInput(page);
      await box.tap();
      await checkRule('schedule-picker', (q) => box.fill(q), () => expect(page.locator('.pmf__dropdown-head')).toContainText('0 participants'));
    });

    await test.step('the schedule\'s text filter keeps the matches that mention the query', async () => {
      await page.goto('/schedule');
      await expect(scheduleRows(page)).toHaveCount(18);
      const k1 = Object.keys(numberOf).find((n) => numberOf[n] === 'K1');

      // "k": the whole K draw, so every Kendo Open match and not one of Women Open.
      await applyScheduleText(page, 'k');
      await expect(scheduleRows(page)).toHaveCount(12);
      await expect(scheduleCount(page)).toHaveText('12 matches of 18');
      for (const row of await scheduleRows(page).all()) await expect(row).toContainText('Kendo Open');
      record({ journey: 'spectator', surface: 'schedule-rows', query: 'k', rows: await scheduleRows(page).count() });
      await shot(page, 'schedule-rows-k');
      await clearScheduleText(page);

      // "k1": K1's two matches, and none of K10, K11 or K12's.
      await applyScheduleText(page, 'k1');
      await expect(scheduleRows(page)).toHaveCount(2);
      for (const row of await scheduleRows(page).all()) {
        await expect(row).toContainText(`K1 ${k1}`);
        await expect(row).not.toContainText(/\bK1[0-2]\b/);
      }
      record({ journey: 'spectator', surface: 'schedule-rows', query: 'k1', rows: await scheduleRows(page).count() });
      await shot(page, 'schedule-rows-k1');
      await clearScheduleText(page);

      // "1": no match mentions a competitor by a bare digit.
      await applyScheduleText(page, '1');
      await expect(scheduleRows(page)).toHaveCount(0);
      record({ journey: 'spectator', surface: 'schedule-rows', query: '1', rows: await scheduleRows(page).count() });
      await clearScheduleText(page);

      // A dojo fragment: the two matches Ella Quinn (Bristol) fights.
      await applyScheduleText(page, 'bristol');
      await expect(scheduleRows(page)).toHaveCount(2);
      for (const row of await scheduleRows(page).all()) await expect(row).toContainText('Ella Quinn');
      await clearScheduleText(page);
      await expect(scheduleRows(page)).toHaveCount(18);
    });
  });

  test('watchlist: follow a competitor, and the hero and the match card follow the court live', async ({ page }) => {
    await page.goto('/');
    // The app is never reloaded in this test: this marker is lost if it is.
    await page.evaluate(() => { window.__spectatorNoReload = 'kept'; });
    // Court A's first bout, and the one after it (read off the operator's
    // console). The followed competitor is the one in BOTH: named from the
    // queue, not assumed to be a given side of the first bout.
    await openShiaijo(op, 'A');
    const first = await sides(upNextCard(op));
    expect(first).toEqual(ids.first);
    const queued = [];
    for (const row of await waitingRows(op).all()) queued.push(await sides(row));
    const next = queued.find((q) => q.shiro !== first.shiro || q.aka !== first.aka);
    expect(next, 'a second bout is queued on court A behind the first').toBeTruthy();
    const followed = [first.shiro, first.aka].find((n) => n === next.shiro || n === next.aka);
    expect(followed, `a competitor is in both court A's first bout (${first.shiro} v ${first.aka}) and its next (${next.shiro} v ${next.aka})`).toBeTruthy();

    await test.step('follow the competitor who fights both of court A\'s first two bouts: the hero shows the match waiting', async () => {
      await watchInput(page).fill(followed.toLowerCase().slice(0, 5));
      await pickerOption(page, followed).tap();
      await expect(watchHero(page)).toContainText(followed);
      await expect(watchHero(page)).toContainText(first.shiro);
      await expect(watchHero(page)).not.toHaveClass(/wl-hero--running/);
      await shot(page, 'watching-waiting');
    });

    await test.step('the operator starts the match: the hero turns to "On court now"', async () => {
      expect(await startUpNext(op)).toEqual(first);
      await expect(watchHero(page)).toHaveClass(/wl-hero--running/);
      await expect(watchHero(page)).toContainText('On court now');
      await shot(page, 'watching-on-court');
    });

    const card = matchCard(page);
    await test.step('the match card opened from the hero follows the operator\'s marks', async () => {
      await watchHeroMatch(page).tap();
      await expect(card).toBeVisible();
      await expect(card).toContainText('NOW');
      await expect(cardSlots(card)).toHaveCount(4);
      expect(await cardMarks(card)).toEqual({ shiro: '', aka: '' });
      await awardIppon(op, 'shiro', 'M');
      await expect.poll(() => cardMarks(card)).toEqual({ shiro: 'M', aka: '' });
      await shot(page, 'watching-card-live');
      record({ journey: 'spectator', step: 'watchlist card live', marks: await cardMarks(card) });
    });

    await test.step('the operator finishes the match: the open card reads FINAL, the hero moves to the next bout', async () => {
      const started = await sides(upNextCard(op));
      expect(started, 'the next bout is the one read off the queue').toEqual(next);
      const opponent = started.shiro === followed ? started.aka : started.shiro;
      await finishMatch(op);
      await expect(card).toContainText('FINAL');
      expect(await cardMarks(card)).toEqual({ shiro: 'M', aka: '' });
      await shot(page, 'watching-card-final');
      // Close the card (the tap that opened it was a while ago).
      await card.getByRole('button', { name: 'Close' }).tap();
      await expect(card).toHaveCount(0);
      // Finish + Start Next started the next bout, also the followed one's: the hero follows.
      await expect(watchHero(page)).toContainText(opponent);
      await expect(watchHero(page)).toHaveClass(/wl-hero--running/);
      await shot(page, 'watching-next-bout');
    });

    expect(await page.evaluate(() => window.__spectatorNoReload), 'the page was never reloaded').toBe('kept');
  });

  test('the match card reads the live row from every door: Home, Schedule, a competition\'s Pools tab and its Overview', async ({ page }) => {
    // The second bout is running on court A since the last test's Finish +
    // Start Next; the third waits as Up next.
    const match2 = await editorSides(op);
    const match3 = await sides(upNextCard(op));
    expect(match3, 'the third bout is another pairing').not.toEqual(match2);
    const card = matchCard(page);

    // Open the card with `open()`, check what it shows now, let the operator
    // change the score, and check the OPEN card shows the change at once.
    const door = async (name, open, before, change, after) => {
      await test.step(name, async () => {
        await open();
        await expect(card).toBeVisible();
        // The card takes no tap within the app's bounce window of the tap that
        // opened it (useOpenedTapGuard), so the dwell runs from when the card
        // was seen. Timed from before open() (a page load and a tab tap ago),
        // it was over before the card showed, and a Close tapped right after a
        // quick change landed inside the window and was swallowed (about one
        // run in five on the Pools door).
        const opened = Date.now();
        await expect.poll(() => cardMarks(card), { message: `${name}: the card opens on the live score` }).toEqual(before);
        await change();
        await expect.poll(() => cardMarks(card), { message: `${name}: the open card follows the operator` }).toEqual(after);
        await shot(page, `door-${name.split(' ')[0].toLowerCase().replace(':', '')}`);
        record({ journey: 'spectator', door: name, before, after });
        await dwell(page, opened);
        await card.getByRole('button', { name: 'Close' }).tap();
        await expect(card).toHaveCount(0);
      });
    };

    await door('Home: the NOW list',
      async () => { await page.goto('/'); await homeNowRow(page).tap(); },
      { shiro: '', aka: '' },
      () => awardIppon(op, 'shiro', 'M'),
      { shiro: 'M', aka: '' });

    await door('Schedule: a row',
      async () => { await page.goto('/schedule'); await scheduleRow(page, match2).tap(); },
      { shiro: 'M', aka: '' },
      () => awardIppon(op, 'aka', 'K'),
      { shiro: 'M', aka: 'K' });

    await door('Pools: a match row on the competition page',
      async () => {
        await page.goto(`/competition/${ids.kendo}`);
        await competitionTab(page, 'Pools').tap();
        await poolRow(page, match2).tap();
      },
      { shiro: 'M', aka: 'K' },
      () => awardIppon(op, 'shiro', 'D'),
      { shiro: 'DM', aka: 'K' });

    // The Overview is the one door that is no dialog while the tournament is
    // officiated: an Up next row expands a card in place (the dialog is the
    // self-run page's), and the running match is its own inline ON NOW card.
    // Both read the page's live rows. The operator scores on, then finishes the
    // running bout so the waiting one (the expanded row) starts.
    await test.step('Overview: an Up next row expands a card; the ON NOW card follows the operator', async () => {
      await page.goto(`/competition/${ids.kendo}`);
      await upNextRow(page, match3).tap();
      const expanded = expandedCard(page);
      await expect(expanded).toBeVisible();
      await expect(expanded).not.toContainText('NOW');
      await expect(cardSlots(expanded)).toHaveCount(4);
      expect(await cardMarks(expanded)).toEqual({ shiro: '', aka: '' });
      await expect.poll(() => cardMarks(nowCard(page)), { message: 'Overview: the ON NOW card opens on the live score' })
        .toEqual({ shiro: 'DM', aka: 'K' });
      // The operator takes Aka's mark back (Shiro's two ippon already decide the bout).
      await clearMarks(filledSlots(op, 'aka'));
      await settled(op);
      await expect.poll(() => cardMarks(nowCard(page)), { message: 'Overview: the ON NOW card follows the operator' })
        .toEqual({ shiro: 'DM', aka: '' });
      await shot(page, 'door-overview-now');

      await finishMatch(op);
      await expect.poll(() => editorSides(op), { message: 'the operator\'s editor holds the third bout' }).toEqual(match3);
      await expect(nowCard(page), 'the ON NOW card moves to the started bout').toContainText(match3.aka);
      await expect(nowCard(page), 'and shows both of its sides, not the finished bout\'s').toContainText(match3.shiro);
      await expect(expanded, 'the waiting bout is no longer in Up next').toHaveCount(0);
      expect(await cardMarks(nowCard(page))).toEqual({ shiro: '', aka: '' });
      await awardIppon(op, 'shiro', 'M');
      await expect.poll(() => cardMarks(nowCard(page)), { message: 'Overview: the ON NOW card follows the next bout' })
        .toEqual({ shiro: 'M', aka: '' });
      await shot(page, 'door-overview-next-bout');
      record({ journey: 'spectator', door: 'Overview: Up next row, ON NOW card', after: await cardMarks(nowCard(page)) });
    });
  });

  test('a change this device cannot send shows on its public page, and goes once it is sent (bc-sypl)', async () => {
    // A held write on the operator device surfaces on its own public page.
    await openShiaijo(op, 'A');
    // The console's queue loads after its heading: wait for a running bout or
    // an Up next card before reading which one it shows.
    await expect(inlineEditor(op).or(upNextCard(op)).first()).toBeVisible();
    if (!(await inlineEditor(op).isVisible())) {
      await startUpNext(op);
    }
    await running(op);
    await settled(op);

    const pub = await opCtx.newPage();
    try {
      await pub.goto('/');
      await expect(pub.locator('.viewer__body')).toBeVisible();
      await expect(pub.getByTestId('public-held')).toHaveCount(0);

      const side = 'aka';
      const before = await filledSlots(op, side).count();

      await opCtx.setOffline(true);
      try {
        await ipponButton(op, side, 'M').tap();
        await expect(filledSlots(op, side)).toHaveCount(before + 1);

        const count = await held(pub, { count: 1 });
        await expect(count).toHaveText(/1 change not sent/);
        const opened = Date.now();
        await count.tap();

        const dialog = pub.getByRole('dialog', { name: 'Held on this device' });
        await expect(dialog).toBeVisible();
        await expect(dialog.getByTestId('held-write')).toHaveCount(1);
        await expect(dialog.getByText('Nothing is held on this device: everything has been sent.')).toHaveCount(0);

        await shot(pub, 'public-held-list');
        await dwell(pub, opened);
        await dialog.getByRole('button', { name: 'Close' }).tap();
        await expect(dialog).toHaveCount(0);
      } finally {
        await opCtx.setOffline(false);
      }

      await notHeld(pub);
      await settled(op);
      await expect(filledSlots(op, side)).toHaveCount(before + 1);
      record({ journey: 'spectator', step: 'held writes on public page', count: '1 change not sent' });
    } finally {
      await pub.close();
    }
  });
});
