// Engi (kata pairs) journey: a knockout of four pairs, scored in flags, and a
// league of four pairs whose tie at 1st the court console decides by flags.
//
// The tournament, the engi competition (the wizard's "Engi (kata competition)"
// box), the roster of pairs ("Name 1 - Name 2, Dojo"), the draw and the start
// are all seeded through the interface, once, in beforeAll. The tests then
// walk the court console: one scores a match and checks the result on a
// spectator's phone, the other guards bc-evts, the engi editor's counterpart
// of bc-ltap (a flag taken back while the save of the one before is still out
// must stay taken back). The last test seeds a league of its own on shiaijo B
// and plays it on that court's console: two pairs end level at 1st (the same
// wins and the same flags), the engine adds their tie-breaker, and the
// console's evidence is named by the flow (engi-tie-at-1st-console-up-next,
// -running, -before-finish, -after; engi-tie-at-1st-desk-table and
// -public-awards). The last test is a SOLO engi competition (the wizard's "One
// competitor per side" box, for iaido and other solo kata): one-name rows go in
// through the paste box and the add form, a pair row is refused with the
// settings sentence at both doors, and a flag bout between two solo competitors
// is scored on the court console and read on a spectator's phone.
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE, EXPECT_TIMEOUT_MS } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { toastText } from '../fixtures/roster.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { openShiaijo, completedRows, sides, upNextCard } from '../fixtures/shiaijo.mjs';
import { INLINE_EDITOR } from '../fixtures/scoring.mjs';
import { openViewer } from '../fixtures/public.mjs';
import { holdScoreWrites } from '../fixtures/held-write.mjs';
import {
  engiEditor, engiPair, engiSettle, engiTotal, enterFlags, flagCount, flagButton, saveEngiResult, startUpNextEngi,
} from '../fixtures/engi.mjs';
import { openLeague, openViewerAwards, readLeagueTable, readNumberedRows, readPodium, seedLeague } from '../fixtures/league.mjs';

// Four pairs, one dojo each: (name, dojo) is unique per pair.
const PAIRS = [
  'Akira Tanaka - Ben Carter, Gyokusen',
  'Chie Mori - Dan Evans, Musashi',
  'Eri Kato - Finn Hale, Seishinkan',
  'Goro Abe - Hana Ito, Thames',
];

let compId;
// The winner's tick (WinnerTick, side_cell.jsx) is an icon; the words it
// carries for a screen reader are what a row's text holds: "Winner: " before
// the name, ", winner" after it.
const WINNER_MARK = /Winner: |, winner\b/g;
const audit = auditLog('engi');
const { record } = audit;
let shot;

test.describe.configure({ mode: 'serial' });

test.describe('engi', () => {
  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    shot = journeyShots('engi');
    const server = await serverForFile(testInfo.file);
    const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: server.base });
    try {
      const page = await ctx.newPage();
      await createTournament(page, { name: 'Engi Cup', courts: 2 });
      compId = await createCompetition(page, {
        name: 'Engi Pairs', kind: 'individual', format: 'knockout', engi: true, courts: ['A'], numberPrefix: 'EN',
      });
      await pasteRoster(page, compId, PAIRS);
      await generateDraw(page, compId);
      await startCompetition(page, compId);
    } finally {
      await ctx.close();
    }
  });

  test.afterAll(() => audit.flush());

  test('score an engi match on the court console', async ({ page, browser, baseURL }) => {
    await login(page);
    await openShiaijo(page, 'A');
    await shot(page, 'court-before-start');

    const pair = await startUpNextEngi(page);
    await expect(engiTotal(page)).toContainText('Enter flags');
    await shot(page, 'engi-editor-started');

    await enterFlags(page, { aka: 2, shiro: 1 });
    await expect(engiTotal(page)).toHaveText('Total: 3, Aka wins');
    await engiSettle(page);
    await shot(page, 'flags-entered');

    const landed = await saveEngiResult(page);
    record({ journey: 'engi', action: 'Save result', pair, landed: landed.detail });
    await expect(completedRows(page)).toHaveCount(1);
    const row = completedRows(page).first();
    await expect(row).toContainText(pair.aka);
    // Aka's side carries the winner mark (the row's text names the side it is
    // on). The Save goes through the outbox and never waits, and the row shows
    // the match finished with the winner its flags decide from the moment it
    // is on the Completed list, so it is read once, straight away, never polled.
    const readRow = async () => (await row.innerText()).replace(/\s+/g, ' ');
    const rowText = await readRow();
    expect(rowText, 'Aka, who held the two flags, is marked the winner').toContain(`Aka: Winner: ${pair.aka}`);
    expect(rowText.match(WINNER_MARK), 'and only Aka').toHaveLength(1);
    const result = (await row.locator('.shiaijo-qrow__result').innerText()).trim();
    record({ journey: 'engi', action: 'console result row', result });
    expect(result, 'the result reads in flags, Shiro 1 and Aka 2').toBe('Flags1–2');
    await shot(page, 'console-after-save');

    const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    try {
      const phone = await spectator.newPage();
      await openViewer(phone, compId);
      const resultCard = phone.locator('button.vsched-item').filter({ hasText: pair.aka }).filter({ hasText: pair.shiro }).first();
      await expect(resultCard).toBeVisible();
      const cardText = (await resultCard.innerText()).replace(/\s+/g, ' ');
      record({ journey: 'engi', action: 'public viewer row', text: cardText });
      expect(cardText, 'the phone shows the flags, Shiro 1 and Aka 2').toContain('1–2');
      expect(cardText, 'Aka is marked the winner').toMatch(new RegExp(`Aka: ${pair.aka}(?:(?!Winner: |, winner).)*, winner`));
      expect(cardText.match(WINNER_MARK), 'and only Aka').toHaveLength(1);
      await shot(phone, 'viewer-engi-result', { fullPage: true });
    } finally {
      await spectator.close();
    }
  });

  test('bc-evts: a flag taken back while its add is saving stays taken back, and is saved', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');
    // The previous test's Finish + Start Next left the second semi-final running;
    // when this test runs alone the Up next card shows instead. Wait for either,
    // and start the Up next match only when the editor is not the one showing.
    const upNextStart = upNextCard(page).getByRole('button', { name: 'Start match' });
    await expect(engiEditor(page).or(upNextStart)).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
    if (!(await engiEditor(page).isVisible())) await startUpNextEngi(page);
    await expect(engiEditor(page)).toBeVisible();
    await expect(flagCount(page, 'aka')).toHaveText('0');
    const writes = await holdScoreWrites(page);
    const lastTap = () => writes.events[writes.events.length - 1];
    const reloaded = async () => {
      await page.reload();
      await expect(engiEditor(page)).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      return Number(await flagCount(page, 'aka').innerText());
    };
    try {
      // Add one, and take it back while the add's save is still out.
      let held = writes.holdNext();
      await flagButton(page, 'aka', '+').tap();
      await held.sent; // the add's tap has left the outbox and is held on the wire
      await flagButton(page, 'aka', '-').tap();
      held.release();
      // The add's answer reached the page before the removal's own tap went out.
      await writes.answeredBeforeNextWrite(held);
      await engiSettle(page);
      // Deliberate: a late echo that undoes the tap-back must be given the time to land, so the time is the evidence.
      await page.waitForTimeout(1000);
      await expect(flagCount(page, 'aka'), 'the taken-back flag stays taken back').toHaveText('0');
      await shot(page, 'flag-taken-back-kept');
      expect(await reloaded(), 'and the server holds no flag').toBe(0);
      // Aka is side a of the match (the score-events wire).
      expect(lastTap(), 'the last tap sent was the removal of Aka\'s flag').toMatchObject({ kind: 'flag.remove', side: 'a' });
      await shot(page, 'flag-taken-back-after-reload');

      // The mirror: from one flag saved, take it off, and put it back while
      // the removal's save is out.
      await flagButton(page, 'aka', '+').tap();
      await engiSettle(page);
      await expect(flagCount(page, 'aka')).toHaveText('1');
      held = writes.holdNext();
      await flagButton(page, 'aka', '-').tap();
      await held.sent;
      await flagButton(page, 'aka', '+').tap();
      held.release();
      await writes.answeredBeforeNextWrite(held);
      await engiSettle(page);
      // Deliberate: a late echo that undoes the tap-back must be given the time to land, so the time is the evidence.
      await page.waitForTimeout(1000);
      await expect(flagCount(page, 'aka'), 'the flag put back stays').toHaveText('1');
      await shot(page, 'flag-put-back-kept');
      expect(await reloaded(), 'and the server holds it').toBe(1);
      expect(lastTap(), 'the last tap sent was the add of Aka\'s flag').toMatchObject({ kind: 'flag.add', side: 'a' });
    } finally {
      await writes.stop();
    }
    writes.assertNoSafetyRelease(); // after the body succeeded: a throw in the finally would hide its error
  });

  // An engi LEAGUE of four pairs on shiaijo B, every bout scored in flags on
  // the court console. By the first member's name (a pair's card shows it
  // first): Ann v Ben 0-3, Ann v Cas 3-0, Ann v Dee 3-0, Ben v Cas 3-0,
  // Ben v Dee 0-3, Cas v Dee 3-2. Ann and Ben end level at 1st, two wins and
  // six flags each; Dee is 3rd (one win, five flags) and Cas 4th (one win, three
  // flags). The engine adds the Ann v Ben tie-breaker, which Ann wins 2-1.
  test('an engi league tie at 1st is decided by flags on the court console', async ({ page, browser, baseURL }) => {
    test.setTimeout(360_000);
    const flow = 'engi-tie-at-1st';
    const LEAGUE = [
      ['Ann Sato - Aya Kudo', 'Gyokusen'],
      ['Ben Ono - Bo Ueda', 'Musashi'],
      ['Cas Hori - Cy Endo', 'Seishinkan'],
      ['Dee Oda - Di Sano', 'Thames'],
    ];
    const first = (name) => name.split(' - ')[0].trim();
    const [ANN, BEN, CAS, DEE] = LEAGUE.map(([n]) => first(n));
    // Flags per bout, keyed by the two first members sorted: { [member]: flags }.
    const key = (a, b) => [a, b].sort().join('|');
    const FLAGS = new Map([
      [key(ANN, BEN), { [ANN]: 0, [BEN]: 3 }],
      [key(ANN, CAS), { [ANN]: 3, [CAS]: 0 }],
      [key(ANN, DEE), { [ANN]: 3, [DEE]: 0 }],
      [key(BEN, CAS), { [BEN]: 3, [CAS]: 0 }],
      [key(BEN, DEE), { [BEN]: 0, [DEE]: 3 }],
      [key(CAS, DEE), { [CAS]: 3, [DEE]: 2 }],
    ]);
    // A side's first member as the console shows it: the editor's pair line,
    // or the first member of the whole pair name the Up next card carries.
    const start = upNextCard(page).getByRole('button', { name: 'Start match' });
    const offeredPair = () => sides(upNextCard(page))
      .then((p) => ({ shiro: first(p.shiro), aka: first(p.aka) })).catch(() => null);
    // Used where no bout runs, so the card, when shown, is the match to play.
    const editorOrCard = async () => ((await start.isVisible().catch(() => false))
      ? offeredPair()
      : engiPair(page).catch(() => null));
    // The Up next card offers Start match beside a RUNNING bout too (starting it
    // sends the running one back to the queue), so a visible card does not say
    // no bout runs. The editor holds the running bout unless it holds the very
    // pair the card offers, which is a bout not started yet.
    const startOrHeld = async () => {
      await expect(engiEditor(page).or(start).first()).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      const held = (await engiEditor(page).isVisible()) ? await engiPair(page) : null;
      const offered = (await start.isVisible()) ? await offeredPair() : null;
      if (held && !(offered && offered.shiro === held.shiro && offered.aka === held.aka)) return held;
      return startUpNextEngi(page);
    };
    let id;

    await login(page);
    id = await seedLeague(page, { name: 'Engi League', kind: 'individual', engi: true, entrants: LEAGUE, courts: ['B'], numberPrefix: 'EL', twoThirdPlaces: true });

    await test.step('the court console scores the six bouts in flags', async () => {
      await openShiaijo(page, 'B');
      const played = [];
      for (let n = 1; n <= 6; n += 1) {
        const pair = await startOrHeld();
        const flags = FLAGS.get(key(pair.shiro, pair.aka));
        expect(flags, `a scripted bout for ${pair.shiro} v ${pair.aka}`).toBeTruthy();
        await enterFlags(page, { shiro: flags[pair.shiro], aka: flags[pair.aka] });
        await engiSettle(page);
        await saveEngiResult(page);
        await expect(completedRows(page)).toHaveCount(n);
        played.push({ ...pair, flags });
      }
      record({ journey: 'engi', flow, action: 'six league bouts on the court console', played });
    });

    await test.step('the desk: Ann and Ben level at 1st, and their tie-breaker is added', async () => {
      await openLeague(page, id);
      await expect.poll(async () => (await readNumberedRows(page)).length, { message: 'the tie-breaker is added', timeout: EXPECT_TIMEOUT_MS }).toBe(7);
      const table = await readLeagueTable(page);
      const tb = (await readNumberedRows(page)).at(-1);
      record({ journey: 'engi', flow, action: 'the table once the six are played', table, tiebreaker: tb,
        screenshot: await shot(page, `${flow}-desk-banner`, { fullPage: true }) });
      expect(table.heading).toBe('Standings');
      expect([first(tb.shiro), first(tb.aka)].sort()).toEqual([ANN, BEN].sort());
    });

    const shots = {};
    let tbPair;
    await test.step('the court console: the tie-breaker is Up next, runs, is decided 2-1 in flags for Ann, and finishes', async () => {
      await openShiaijo(page, 'B');
      await expect.poll(async () => {
        const p = await editorOrCard();
        return !!p && [p.shiro, p.aka].sort().join('|') === key(ANN, BEN);
      }, { message: 'the console holds the Ann v Ben tie-breaker', timeout: EXPECT_TIMEOUT_MS }).toBe(true);
      const upNext = (await upNextCard(page).count()) ? (await upNextCard(page).first().innerText()).replace(/\s+/g, ' ').trim() : '';
      shots.upNext = await shot(page, `${flow}-console-up-next`, { fullPage: true });
      const startTb = upNextCard(page).getByRole('button', { name: 'Start match' });
      if (await startTb.isVisible()) await startTb.tap();
      // fixed (E1): the tie-breaker ("Pool A-TB-1") opens the flag editor, as
      // the league bouts do, never the kendo ippon editor (M K D T H, fouls,
      // hantei). Waiting on either editor lets a regression fail on the
      // assertion below rather than on a timeout.
      const ipponEditor = page.locator(INLINE_EDITOR).first().getByTestId('scoring-modal-mark-draw');
      await expect(engiEditor(page).or(ipponEditor).first()).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      const flagsEditor = await engiEditor(page).isVisible();
      shots.running = await shot(page, `${flow}-console-running`, { fullPage: true });
      record({ journey: 'engi', flow, action: 'the tie-breaker started on the court console', upNext, flagsEditor, screenshot: shots.running });
      expect(flagsEditor, 'the engi tie-breaker opens the flag editor, not the ippon editor').toBe(true);
      tbPair = await engiPair(page);
      await enterFlags(page, { shiro: tbPair.shiro === ANN ? 2 : 1, aka: tbPair.aka === ANN ? 2 : 1 });
      await engiSettle(page);
      const total = (await engiTotal(page).innerText()).trim();
      shots.beforeFinish = await shot(page, `${flow}-console-before-finish`, { fullPage: true });
      await saveEngiResult(page);
      await expect(completedRows(page)).toHaveCount(7);
      shots.after = await shot(page, `${flow}-console-after`, { fullPage: true });
      record({ journey: 'engi', flow, action: 'the tie-breaker on the court console', upNext, pair: tbPair, total, shots });
      expect(upNext === '' || /tie-?break/i.test(upNext), 'the Up next card says it is a tie-breaker').toBe(true);
      expect(total).toBe(`Total: 3, ${tbPair.shiro === ANN ? 'Shiro' : 'Aka'} wins`);
    });

    await test.step('the desk reads Final standings with Ann first; the phone\'s Awards agree', async () => {
      await openLeague(page, id);
      await expect.poll(async () => (await readLeagueTable(page)).heading, { message: 'the league finished', timeout: EXPECT_TIMEOUT_MS }).toBe('Final standings');
      const table = await readLeagueTable(page);
      const rank = (member) => table.rows.find((r) => r.name.startsWith(member))?.rank;
      record({ journey: 'engi', flow, action: 'Final standings', table, screenshot: await shot(page, `${flow}-desk-table`, { fullPage: true }) });
      expect([ANN, BEN, DEE, CAS].map(rank)).toEqual([1, 2, 3, 4]);
      expect(table.winner.startsWith(ANN), `the winner is Ann's pair (${table.winner})`).toBe(true);
      const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
      try {
        const phone = await spectator.newPage();
        await openViewerAwards(phone, id);
        const podium = await readPodium(phone);
        record({ journey: 'engi', flow, action: 'public Awards', podium, screenshot: await shot(phone, `${flow}-public-awards`, { fullPage: true }) });
        expect(podium.places.slice(0, 3).map((pl) => first(pl.name))).toEqual([ANN, BEN, DEE]);
      } finally {
        await spectator.close();
      }
    });
  });

  // A SOLO engi competition on shiaijo B, after the league above has finished
  // there. The wizard's "One competitor per side" box makes every side one
  // person: a roster row is a single name ("Name, Dojo"), and a pair row
  // ("Name 1 - Name 2, Dojo") is refused at every door with the sentence that
  // sends the organiser to Settings.
  test('a solo engi competition takes one name per side and scores a flag bout', async ({ page, browser, baseURL }) => {
    test.setTimeout(240_000);
    const flow = 'engi-solo';
    const PAIR_ROW = 'Akira Tanaka - Ben Carter, Gyokusen';
    const SOLO = [
      ['Akira Tanaka', 'Gyokusen'],
      ['Ben Carter', 'Musashi'],
      ['Chie Mori', 'Seishinkan'],
    ];
    const ADDED = ['Dan Evans', 'Thames'];
    // The sentence a solo competition answers for a pair (engi_entry.jsx).
    const PAIR_REFUSED = /one competitor per side.*turn off "One competitor per side" in Settings/;

    await login(page);
    const id = await createCompetition(page, {
      name: 'Engi Solo', kind: 'individual', format: 'knockout', engi: true, engiSolo: true, courts: ['B'], numberPrefix: 'ES',
    });

    await test.step('the paste box refuses a pair row, then takes one-name rows', async () => {
      const box = page.locator('.lined-textarea__area');
      await box.fill(PAIR_ROW);
      await page.getByRole('button', { name: 'Apply changes' }).first().tap();
      const refusal = await toastText(page, PAIR_REFUSED);
      record({ journey: 'engi', flow, action: 'a pair row pasted into a solo competition', refusal });
      await shot(page, `${flow}-paste-refused`);
      await pasteRoster(page, id, SOLO);
    });

    await test.step('the add form refuses a pair name, then adds one name', async () => {
      await page.goto(`/admin/competition/${id}/participants`);
      await page.getByRole('button', { name: '+ Add participant' }).tap();
      const name = page.getByLabel(/^Name \*/);
      const dojo = page.getByLabel(/^Dojo \*/);
      const add = page.getByRole('button', { name: 'Add', exact: true });
      await name.fill('Dan Evans - Eri Kato');
      await dojo.fill(ADDED[1]);
      await add.tap();
      const refusal = await toastText(page, PAIR_REFUSED);
      record({ journey: 'engi', flow, action: 'a pair name typed into the add form', refusal });
      await name.fill(ADDED[0]);
      await add.tap();
      await toastText(page, new RegExp(`${ADDED[0]} added`));
      await expect(page.getByRole('button', { name: `Edit ${ADDED[0]}` })).toBeVisible();
      await shot(page, `${flow}-roster`);
    });

    await generateDraw(page, id);
    await startCompetition(page, id);

    let pair;
    await test.step('the court console scores a flag bout between two solo competitors', async () => {
      await openShiaijo(page, 'B');
      pair = await startUpNextEngi(page);
      for (const side of ['shiro', 'aka']) {
        expect(pair[side], `the ${side} side holds one competitor, never a pair`).not.toContain(' - ');
      }
      await enterFlags(page, { aka: 2, shiro: 1 });
      await expect(engiTotal(page)).toHaveText('Total: 3, Aka wins');
      await engiSettle(page);
      await shot(page, `${flow}-flags-entered`);
      const landed = await saveEngiResult(page);
      record({ journey: 'engi', flow, action: 'Save result', pair, landed: landed.detail });
      await expect(completedRows(page)).toHaveCount(1);
      const rowText = (await completedRows(page).first().innerText()).replace(/\s+/g, ' ');
      expect(rowText, 'Aka, who held the two flags, is marked the winner').toContain(`Aka: Winner: ${pair.aka}`);
      expect(rowText.match(WINNER_MARK), 'and only Aka').toHaveLength(1);
      const result = (await completedRows(page).first().locator('.shiaijo-qrow__result').innerText()).trim();
      expect(result, 'the result reads in flags, Shiro 1 and Aka 2').toBe('Flags1–2');
      await shot(page, `${flow}-console-after-save`);
    });

    await test.step('the phone shows the same result', async () => {
      const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
      try {
        const phone = await spectator.newPage();
        await openViewer(phone, id);
        const card = phone.locator('button.vsched-item').filter({ hasText: pair.aka }).filter({ hasText: pair.shiro }).first();
        await expect(card).toBeVisible();
        const text = (await card.innerText()).replace(/\s+/g, ' ');
        record({ journey: 'engi', flow, action: 'public viewer row', text });
        expect(text, 'the phone shows the flags, Shiro 1 and Aka 2').toContain('1–2');
        expect(text.match(WINNER_MARK), 'and marks only Aka the winner').toHaveLength(1);
        await shot(phone, `${flow}-viewer-result`, { fullPage: true });
      } finally {
        await spectator.close();
      }
    });
  });
});
