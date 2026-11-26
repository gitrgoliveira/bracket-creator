// Concurrent taps: two devices (two OPERATOR_DEVICE contexts, two iPads)
// score the same running match at the same time (bc-evts).
//
// Device 1 is the court's tablet: the court console of the match's shiaijo
// (/admin/shiaijo/:court), which runs that shiaijo. Device 2 scores the same
// match from the competition's Scores page (/admin/competition/:id/scores),
// the overlay editor the head table uses. One tab runs each shiaijo (FR9): a
// court console on a shiaijo another tablet runs is view only for every write,
// so two court consoles can no longer score one match together, while the
// Scores page, an admin surface, still writes. The second device of every case
// is therefore that admin surface, never a second court console. Each device
// is its own browser context, so its own tablet with its own outbox and event
// session (pages of one context share storage and would be one tablet).
//
// Every scoring tap is an event the device queues in its outbox and sends to
// POST /matches/:id/score-events; the server appends it to the match's score
// log and replays the log into the match ordered by each tap's stamp, not by
// arrival. So two taps made within a moment of each other on two devices both
// count, a device that was offline sends its taps when it is back, and a mark
// taken off and put back while the first tap's answer is still out ends as the
// operator left it. Each case is asserted on both devices' boards, and the
// first also on the TV board and in the match history.
//
//   a  individual: device 1 taps Shiro M and device 2 taps Aka K within
//      300 ms; both editors, the TV board and the History list show both
//   b  offline: device 2 goes offline, taps, comes back; its tap is sent and
//      both devices converge
//   c  remove then retap during the echo: a mark taken off and tapped back
//      while the removal's batch is held, and the reverse, end with exactly
//      what the operator did last, on both devices and after a reload
//   d  engi: +Shiro on one device and +Aka on the other end 1-1 on both
//   e  team: the two devices tap on different bouts at once; both bouts keep
//      every tap
//   f  kachinuki: two strikes for Shiro from two devices at once on the
//      encounter's first bout, which the server holds no row for yet; both
//      devices show the lineup's fighters on it, a later edit of the match's
//      lineup renames it on both, and Record bout records that bout with both
//      points
//   g  turned away later: device 2's tap is counted, then the court's
//      finish, made offline on device 1 before that tap and never having seen
//      it, lands. Device 1 is the court's tablet, so its result is the court's
//      and counts (FR8); the match was finished before device 2's tap, so
//      device 2 is told its tap was not applied, the alert naming the match
//      and the tap
//   h  a second court console: a console on shiaijo A on another tablet
//      watches (grey bar naming another device, the running match's board and
//      Send back to queue off) until
//      it taps Take control; then the first tablet's bar turns amber, and the
//      same two taps there take it back
//
// Known race, not asserted here: the CAP. A side that holds two points has
// decided its bout (sanbon shobu), so a third strike for it, or any strike for
// the other side, is `capped` and changes nothing
// (internal/engine/score_replay_rules_test.go TestReplay_CappedStrikes). When
// two devices each add the SECOND point to the same side at once (Shiro holds
// M; device 1 taps K, device 2 taps D), the server keeps the one stamped
// first and caps the other: the side ends with exactly two marks on every
// device and after a reload, and the capped tap is in the match's history
// with "the bout was already decided". Which of the two marks survives, and
// which device is told its tap was not applied, follows the two devices'
// stamps, which a journey cannot control, and an earlier-stamped tap that
// arrives late can still swap the survivor after both boards agree. Only the
// count is deterministic; the journey does not assert the race. (The same
// FIRST point tapped on two devices counts twice, by design: two strikes,
// TestReplay_SameStrikeOnTwoDevicesCountsTwice.)
//
// Seeding goes through the interface, once, in beforeAll: one tournament on
// four shiaijo, one competition per format, each on a shiaijo of its own.
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE, EXPECT_TIMEOUT_MS } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { editorIdentity, inlineEditor, openShiaijo, sides, startUpNext, upNextCard } from '../fixtures/shiaijo.mjs';
import { EDITOR, INLINE_EDITOR, armFinish, ipponButton, running, settled, unsentCount } from '../fixtures/scoring.mjs';
import { openRunningScoreEditor } from '../fixtures/scores.mjs';
import { OTHER_DEVICE_TEXT, HOLDS_TEXT, awaitControl, controlState, controlText, takeControl } from '../fixtures/court-control.mjs';
import { toastText } from '../fixtures/roster.mjs';
import { editorSides, inlineRunning, rosterOf, slotMarks } from '../fixtures/individual.mjs';
import { holdScoreWrites } from '../fixtures/held-write.mjs';
import { tapOnSide } from '../fixtures/pace.mjs';
import { openTvBoard, tvBoard } from '../fixtures/public.mjs';
import { engiEditor, engiSettle, flagButton, flagCount, startUpNextEngi } from '../fixtures/engi.mjs';
import * as T from '../fixtures/team.mjs';
import {
  boutIpponButton, boutNames, closeLineupPanel, currentBout, doneRowWinner, doneRows,
  openLineupFromUpNext, recordBout, recordBoutButton, startUpNextKachinuki, typeLineup,
} from '../fixtures/kachinuki.mjs';

const JOURNEY = 'concurrent-taps';

// Two taps count as made "at once" when their dispatches end this close.
const AT_ONCE_MS = 300;

// How many of a tap batch's events are `kind` on Aka (with `letter`, when
// given): a batch body is `{ session, events }` (fixtures/held-write.mjs).
const tapsIn = (body, kind, letter) => ((body && body.events) || [])
  .filter((e) => e.kind === kind && e.side === 'a' && (!letter || e.letter === letter)).length;

const PAIRS = [
  'Akira Tanaka - Ben Carter, Gyokusen',
  'Chie Mori - Dan Evans, Musashi',
  'Eri Kato - Finn Hale, Seishinkan',
  'Goro Abe - Hana Ito, Thames',
];
const TEAMS = [['Team Ume', 'Kita Dojo'], ['Team Sakura', 'Minami Dojo']];
const KACHINUKI_TEAMS = [['Team Taka', 'Higashi Dojo'], ['Team Washi', 'Nishi Dojo']];
const KACHINUKI_MEMBERS = {
  'Team Taka': ['Aoki', 'Baba', 'Chiba'],
  'Team Washi': ['Fujii', 'Goto', 'Hara'],
};

test.describe.configure({ mode: 'serial' });

test.describe('concurrent taps from two devices', () => {
  let shot;
  const audit = auditLog(JOURNEY);
  const { record } = audit;
  const ids = {};

  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    shot = journeyShots(JOURNEY);
    const server = await serverForFile(testInfo.file);
    const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: server.base });
    try {
      const page = await ctx.newPage();
      await createTournament(page, { name: 'Two Tablets Cup', courts: 4 });
      ids.individual = await createCompetition(page, {
        name: 'Two Tablets Individual', kind: 'individual', format: 'knockout', courts: ['A'], numberPrefix: 'TI',
      });
      await pasteRoster(page, ids.individual, rosterOf('Tablet', 4));
      await generateDraw(page, ids.individual);
      await startCompetition(page, ids.individual);
      ids.engi = await createCompetition(page, {
        name: 'Two Tablets Engi', kind: 'individual', format: 'knockout', engi: true, courts: ['B'], numberPrefix: 'TE',
      });
      await pasteRoster(page, ids.engi, PAIRS);
      await generateDraw(page, ids.engi);
      await startCompetition(page, ids.engi);
      ids.team = await createCompetition(page, {
        name: 'Two Tablets Team', kind: 'team', format: 'knockout', teamSize: 3, teamMatchType: 'fixed', courts: ['C'], numberPrefix: 'TT',
      });
      await pasteRoster(page, ids.team, TEAMS);
      await generateDraw(page, ids.team);
      await startCompetition(page, ids.team);
      ids.kachinuki = await createCompetition(page, {
        name: 'Two Tablets Kachinuki', kind: 'team', format: 'knockout', teamSize: 3, teamMatchType: 'kachinuki', courts: ['D'], numberPrefix: 'TK',
      });
      await pasteRoster(page, ids.kachinuki, KACHINUKI_TEAMS);
      await generateDraw(page, ids.kachinuki);
      await startCompetition(page, ids.kachinuki);
    } finally {
      await ctx.close();
    }
  });

  test.afterAll(() => audit.flush());

  // Two signed-in operator iPads, each on its own context (its own tablet:
  // its own outbox, its own event session). Device 1 opens the court console
  // of `court` and runs that shiaijo; device 2 is signed in and opens the
  // running match of competition `id` from the Scores page with openD2(),
  // once the match runs (again after a reload: the page does not keep the
  // editor open).
  async function twoDevices(browser, baseURL, court, id) {
    const contexts = [];
    const pages = [];
    for (let i = 0; i < 2; i += 1) {
      const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL });
      contexts.push(ctx);
      const page = await ctx.newPage();
      await login(page);
      pages.push(page);
    }
    const [d1, d2] = pages;
    const { state } = await openShiaijo(d1, court);
    expect(state, `device 1 runs shiaijo ${court}`).toBe('holds');
    const openD2 = async () => {
      await openRunningScoreEditor(d2, id, { court });
      await running(d2, EDITOR);
    };
    return { d1, d2, ctx1: contexts[0], ctx2: contexts[1], openD2, close: () => Promise.all(contexts.map((c) => c.close())) };
  }

  // The two editors: device 1's inline one on the court console, device 2's
  // overlay on the Scores page.
  const ROOT = { d1: INLINE_EDITOR, d2: EDITOR };

  // Tap two controls, on two devices, at once, and return how far apart the
  // two dispatches ended. Throws when they were not within AT_ONCE_MS: the row
  // would then not test concurrent taps, and a check built on it would pass
  // without testing anything.
  async function tapAtOnce(a, b) {
    const ended = async (loc) => { await loc.tap(); return Date.now(); };
    const [ta, tb] = await Promise.all([ended(a), ended(b)]);
    const gapMs = Math.abs(ta - tb);
    if (gapMs > AT_ONCE_MS) {
      throw new Error(`tapAtOnce: the two taps ended ${gapMs} ms apart, not within ${AT_ONCE_MS} ms; the machine was too slow to make them at once`);
    }
    return gapMs;
  }

  // Both devices' editors hold the same running match: the same meta line
  // (competition, round, match) and the same pairing. The titles are not
  // compared: each mount draws its own.
  async function sameMatchOnBoth(d1, d2) {
    await expect.poll(async () => {
      const [a, b] = [await editorIdentity(d1, ROOT.d1).catch(() => null), await editorIdentity(d2, ROOT.d2).catch(() => null)];
      const same = (x) => [x.meta, ...x.names].join(' | ');
      return !!a && !!b && a.names.length === 2 && same(a) === same(b);
    }, { message: 'both devices hold the same match' }).toBe(true);
  }

  // The marks a side of the individual board holds, sorted: a slot is filled
  // outside to inside on each side, so Aka's slot order runs the other way
  // round from the order the points were struck in. `root` is the device's
  // editor (ROOT).
  const marksOf = async (d, side, root = INLINE_EDITOR) => (await slotMarks(d, side, root)).sort();

  // Each device with its name and its editor, for the checks made on both.
  const both = (d1, d2) => [['device 1', d1, ROOT.d1], ['device 2', d2, ROOT.d2]];

  // The marks a side of the individual TV board's current row shows, in slot
  // order, letters only (an empty slot reads a dot or a dash).
  async function tvMarks(tv, side) {
    const row = tvBoard(tv).getByTestId('tvd-indiv-row-now').first();
    const slots = side === 'aka' ? row.locator('.msb-slots--aka .msb-slot') : row.locator('.msb-slots').first().locator('.msb-slot');
    return (await slots.allInnerTexts()).map((t) => t.trim()).filter((t) => /^[A-Z]+$/.test(t));
  }

  test('a individual: Shiro M on one device and Aka K on the other within 300 ms reach both editors, the TV board and the history', async ({ browser, baseURL }) => {
    const ops = await twoDevices(browser, baseURL, 'A', ids.individual);
    const tvCtx = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    try {
      const { d1, d2 } = ops;
      const pair = await startUpNext(d1);
      // Device 2 finds the match running on the Scores page and opens it.
      await ops.openD2();
      await sameMatchOnBoth(d1, d2);
      expect(await editorSides(d2, ROOT.d2)).toEqual(pair);

      const gapMs = await tapAtOnce(ipponButton(d1, 'shiro', 'M'), ipponButton(d2, 'aka', 'K', ROOT.d2));
      // Each device's own tap is answered...
      await settled(d1);
      await settled(d2);
      // ...and both boards show both taps.
      for (const [name, d, root] of both(d1, d2)) {
        await expect.poll(() => slotMarks(d, 'shiro', root), { message: `${name} shows Shiro's M` }).toEqual(['M']);
        await expect.poll(() => slotMarks(d, 'aka', root), { message: `${name} shows Aka's K` }).toEqual(['K']);
      }
      await shot(d1, 'a-device-1');
      await shot(d2, 'a-device-2');

      // The TV board of the court, on another device: one round trip later.
      const tv = await tvCtx.newPage();
      await openTvBoard(tv, 'A');
      await expect(tvBoard(tv)).toContainText(pair.shiro);
      await expect.poll(() => tvMarks(tv, 'shiro'), { message: 'the TV board shows Shiro\'s M' }).toEqual(['M']);
      await expect.poll(() => tvMarks(tv, 'aka'), { message: 'the TV board shows Aka\'s K' }).toEqual(['K']);
      await shot(tv, 'a-tv-board');

      // The match history, under History in the editor: one line per tap.
      const history = inlineEditor(d1).getByTestId('match-history');
      await history.getByTestId('match-history-toggle').tap();
      const body = history.getByTestId('match-history-body');
      await expect(body).toContainText(/Shiro M added/);
      await expect(body).toContainText(/Aka K added/);
      const lines = (await body.getByTestId('match-history-entry').allInnerTexts()).map((t) => t.replace(/\s+/g, ' ').trim());
      await shot(d1, 'a-history');
      record({ journey: JOURNEY, case: 'a individual', pair, gapMs, history: lines });
    } finally {
      await tvCtx.close();
      await ops.close();
    }
  });

  test('b offline: a tap made while one device is offline is sent when it is back, and both devices converge', async ({ browser, baseURL }) => {
    const ops = await twoDevices(browser, baseURL, 'A', ids.individual);
    try {
      const { d1, d2, ctx2 } = ops;
      await expect.poll(() => inlineRunning(d1)).toBe(true);
      await ops.openD2();
      await sameMatchOnBoth(d1, d2);
      await expect.poll(() => marksOf(d2, 'aka', ROOT.d2)).toEqual(['K']);

      let unsentOffline;
      await ctx2.setOffline(true);
      try {
        await tapOnSide(d2, 'aka', ipponButton(d2, 'aka', 'D', ROOT.d2));
        // Device 2 shows its own tap at once, and the outbox still holds the point.
        await expect.poll(() => marksOf(d2, 'aka', ROOT.d2), { message: 'device 2 shows its offline tap' }).toEqual(['D', 'K']);
        await expect.poll(() => unsentCount(d2)).toBeGreaterThan(0);
        unsentOffline = await unsentCount(d2);
        // Device 1 has not seen it: it has not reached the server. Device 1
        // reads the server's copy afresh (a reload), so its board is the
        // server's match as it stands now, which holds Aka's K and not the D.
        await d1.reload();
        // A reload keeps the tab, so the court console still runs its shiaijo.
        expect(await awaitControl(d1), 'device 1 still runs shiaijo A after a reload').toBe('holds');
        await expect.poll(() => inlineRunning(d1), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
        await expect.poll(() => slotMarks(d1, 'shiro'), { message: 'device 1 shows the server\'s copy' }).toEqual(['M']);
        await expect.poll(() => marksOf(d1, 'aka'), { message: 'device 1 cannot see a tap still on device 2' }).toEqual(['K']);
        await shot(d2, 'b-device-2-offline');
      } finally {
        await ctx2.setOffline(false);
      }
      // Back online: the outbox sends the tap, and both boards agree.
      await settled(d2);
      for (const [name, d, root] of both(d1, d2)) {
        await expect.poll(() => marksOf(d, 'aka', root), { message: `${name} shows Aka's K and D` }).toEqual(['D', 'K']);
        await expect.poll(() => slotMarks(d, 'shiro', root), { message: `${name} still shows Shiro's M` }).toEqual(['M']);
      }
      await shot(d1, 'b-device-1-converged');
      record({ journey: JOURNEY, case: 'b offline', unsent: unsentOffline });
    } finally {
      await ops.close();
    }
  });

  test('c a mark taken off and tapped back while the removal is out, and the reverse, end as the operator left them', async ({ browser, baseURL }) => {
    const ops = await twoDevices(browser, baseURL, 'A', ids.individual);
    try {
      const { d1, d2 } = ops;
      await expect.poll(() => inlineRunning(d1)).toBe(true);
      await ops.openD2();
      await sameMatchOnBoth(d1, d2);
      await expect.poll(() => marksOf(d1, 'aka')).toEqual(['D', 'K']);
      const akaMark = (letter) => inlineEditor(d1).locator(`[aria-label^="Aka slot "][aria-label$=": remove ${letter}"]`).first();
      const writes = await holdScoreWrites(d1);
      try {
        // Take D off, and tap it back while the removal's batch is held.
        let held = writes.holdNext();
        await akaMark('D').tap();
        await held.sent;
        await tapOnSide(d1, 'aka', ipponButton(d1, 'aka', 'D'));
        held.release();
        let batches = await writes.answeredBeforeNextWrite(held);
        // The tap-back was made while the removal was out: it is not in the
        // held batch, and it went in the next one.
        expect(tapsIn(batches.held, 'ippon.add', 'D'), 'the tap-back is not in the held removal batch').toBe(0);
        expect(tapsIn(batches.held, 'ippon.remove'), 'the held batch carries the removal').toBe(1);
        expect(tapsIn(batches.next, 'ippon.add', 'D'), 'the tap-back is in the batch after it').toBe(1);
        await settled(d1);
        // Deliberate: a late echo that undoes the tap-back must be given the time to land, so the time is the evidence.
        await d1.waitForTimeout(1000);
        for (const [name, d, root] of both(d1, d2)) {
          await expect.poll(() => marksOf(d, 'aka', root), { message: `${name}: the mark tapped back stays` }).toEqual(['D', 'K']);
        }
        await shot(d2, 'c-tapped-back-on-device-2');
        // Device 2 reads the server's copy afresh: the Scores page again.
        await ops.openD2();
        await expect.poll(() => marksOf(d2, 'aka', ROOT.d2), { message: 'and the server holds the mark tapped back' }).toEqual(['D', 'K']);
        await expect.poll(() => slotMarks(d2, 'shiro', ROOT.d2)).toEqual(['M']);
        await shot(d2, 'c-tapped-back-after-reload');

        // The reverse: D off (answered), then D added and taken off again
        // while the add's batch is held.
        await akaMark('D').tap();
        await settled(d1);
        await expect.poll(() => slotMarks(d1, 'aka')).toEqual(['K']);
        held = writes.holdNext();
        await tapOnSide(d1, 'aka', ipponButton(d1, 'aka', 'D'));
        await held.sent;
        await akaMark('D').tap();
        held.release();
        batches = await writes.answeredBeforeNextWrite(held);
        expect(tapsIn(batches.held, 'ippon.remove'), 'the removal is not in the held add batch').toBe(0);
        expect(tapsIn(batches.held, 'ippon.add', 'D'), 'the held batch carries the add').toBe(1);
        expect(tapsIn(batches.next, 'ippon.remove'), 'the removal is in the batch after it').toBe(1);
        await settled(d1);
        // Deliberate: a late echo that undoes the removal must be given the time to land, so the time is the evidence.
        await d1.waitForTimeout(1000);
        for (const [name, d, root] of both(d1, d2)) {
          await expect.poll(() => slotMarks(d, 'aka', root), { message: `${name}: the mark taken off stays off` }).toEqual(['K']);
        }
        const tail = writes.events.slice(-2).map((e) => `${e.kind} ${e.side}${e.letter ? ` ${e.letter}` : ''}`);
        expect(tail, 'the last two taps sent were the add and the removal').toEqual([
          expect.stringMatching(/^ippon\.add a D$/), expect.stringMatching(/^ippon\.remove a$/),
        ]);
        await ops.openD2();
        await expect.poll(() => slotMarks(d2, 'aka', ROOT.d2), { message: 'and the server holds that' }).toEqual(['K']);
        await expect.poll(() => slotMarks(d2, 'shiro', ROOT.d2)).toEqual(['M']);
        await shot(d2, 'c-after-reload');
        record({ journey: JOURNEY, case: 'c remove then retap', events: writes.events.map((e) => `${e.kind} ${e.side} ${e.letter || ''}`.trim()) });
      } finally {
        await writes.stop();
      }
      writes.assertNoSafetyRelease(); // after the body succeeded: a throw in the finally would hide its error
    } finally {
      await ops.close();
    }
  });

  test('d engi: +Shiro on one device and +Aka on the other at once end 1-1 on both', async ({ browser, baseURL }) => {
    const ops = await twoDevices(browser, baseURL, 'B', ids.engi);
    try {
      const { d1, d2 } = ops;
      await startUpNextEngi(d1);
      await ops.openD2();
      await expect(engiEditor(d2, ROOT.d2)).toBeVisible();
      await sameMatchOnBoth(d1, d2);
      const gapMs = await tapAtOnce(flagButton(d1, 'shiro', '+'), flagButton(d2, 'aka', '+', ROOT.d2));
      await engiSettle(d1);
      await engiSettle(d2);
      for (const [name, d, root] of both(d1, d2)) {
        await expect(flagCount(d, 'shiro', root), `${name}: Shiro's flag`).toHaveText('1');
        await expect(flagCount(d, 'aka', root), `${name}: Aka's flag`).toHaveText('1');
      }
      await shot(d1, 'd-engi-device-1');
      await shot(d2, 'd-engi-device-2');
      record({ journey: JOURNEY, case: 'd engi', gapMs });
    } finally {
      await ops.close();
    }
  });

  test('e team: two devices tapping different bouts at once keep every tap on both bouts', async ({ browser, baseURL }) => {
    const ops = await twoDevices(browser, baseURL, 'C', ids.team);
    try {
      const { d1, d2 } = ops;
      const ed1 = await T.startUpNextTeam(d1);
      await ops.openD2();
      const ed2 = d2.locator(ROOT.d2).first();
      await running(d2, ed2);
      await expect(T.boutRow(ed2, 2)).toBeVisible();
      await sameMatchOnBoth(d1, d2);
      const gaps = [];
      // Round 1: device 1 on bout 1, device 2 on bout 2.
      gaps.push(await tapAtOnce(T.rowIppon(T.boutRow(ed1, 1), 'shiro', 'M'), T.rowIppon(T.boutRow(ed2, 2), 'aka', 'K')));
      // Round 2: the other way round.
      gaps.push(await tapAtOnce(T.rowIppon(T.boutRow(ed1, 2), 'shiro', 'K'), T.rowIppon(T.boutRow(ed2, 1), 'aka', 'M')));
      await settled(d1);
      await settled(d2);
      const want = { 1: { shiro: ['M'], aka: ['M'] }, 2: { shiro: ['K'], aka: ['K'] } };
      for (const [name, ed] of [['device 1', ed1], ['device 2', ed2]]) {
        for (const n of [1, 2]) {
          for (const side of ['shiro', 'aka']) {
            await expect(T.rowSlots(T.boutRow(ed, n), side), `${name}, bout ${n}, ${side}`).toHaveText(want[n][side]);
          }
        }
      }
      await shot(d1, 'e-team-device-1');
      await shot(d2, 'e-team-device-2');
      record({ journey: JOURNEY, case: 'e team', gaps, sheet1: await T.sheetState(ed1, 3), sheet2: await T.sheetState(ed2, 3) });
    } finally {
      await ops.close();
    }
  });

  test('f kachinuki: Record bout after two devices strike for Shiro at once records the bout with both points', async ({ browser, baseURL }) => {
    const ops = await twoDevices(browser, baseURL, 'D', ids.kachinuki);
    try {
      const { d1, d2 } = ops;
      // The lineups first, at the court: the first bout's fighters come from them.
      const pair = await sides(upNextCard(d1));
      await openLineupFromUpNext(d1);
      for (const team of [pair.shiro, pair.aka]) await typeLineup(d1, team, KACHINUKI_MEMBERS[team]);
      await closeLineupPanel(d1);
      await startUpNextKachinuki(d1);
      await ops.openD2();
      await expect(recordBoutButton(d2, ROOT.d2), 'device 2 shows the encounter running').toBeVisible();
      await sameMatchOnBoth(d1, d2);

      // The concurrent taps go on the FIRST bout, which the server holds no row
      // for (the encounter was started from the queue): the server makes the
      // row at the first tap and seats the lineup's fighters on it itself
      // (operator decision: bout 1 is whatever the lineup says), so neither
      // device's strike is turned away and both show the same names.
      const opener = { shiro: KACHINUKI_MEMBERS[pair.shiro][0], aka: KACHINUKI_MEMBERS[pair.aka][0] };
      for (const [name, d, root] of both(d1, d2)) {
        await expect(T.rowSlots(currentBout(d, root), 'shiro'), `${name}: bout 1 starts with no points`).toHaveCount(0);
      }

      const gapMs = await tapAtOnce(boutIpponButton(currentBout(d1), 'shiro', 'M'), boutIpponButton(currentBout(d2, ROOT.d2), 'shiro', 'K'));
      await settled(d1);
      await settled(d2);
      const shiroMarks = async (row) => (await T.rowSlots(row, 'shiro').allInnerTexts()).map((t) => t.trim()).sort();
      // Both devices show both strikes before the bout is recorded, so Record
      // bout is made over a board that holds them.
      for (const [name, d, root] of both(d1, d2)) {
        await expect.poll(() => shiroMarks(currentBout(d, root)), { message: `${name} shows both of Shiro's points` }).toEqual(['K', 'M']);
        await expect.poll(() => boutNames(currentBout(d, root)), { message: `${name} shows the lineup's fighters on bout 1` }).toEqual(opener);
      }
      // A later edit of this match's lineup (Shiro's first fighter typed anew
      // on bout 1's name box) renames bout 1 on both devices; its points stay.
      const renamed = 'Ishii';
      await T.typeIntoNameBox(T.rowNameBox(currentBout(d1), 'shiro'), renamed);
      opener.shiro = renamed;
      for (const [name, d, root] of both(d1, d2)) {
        await expect.poll(() => boutNames(currentBout(d, root)), { message: `${name} shows bout 1 renamed by the lineup edit` }).toEqual(opener);
        await expect.poll(() => shiroMarks(currentBout(d, root)), { message: `${name}: the points stay on bout 1` }).toEqual(['K', 'M']);
      }
      await settled(d1);
      await shot(d1, 'f-kachinuki-before-record');
      await recordBout(d1);
      for (const [name, d, root] of both(d1, d2)) {
        await expect(doneRows(d, root), `${name}: bout 1 is recorded`).toHaveCount(1);
        const done = doneRows(d, root).nth(0);
        await expect.poll(() => shiroMarks(done), { message: `${name}: the recorded bout carries both points` }).toEqual(['K', 'M']);
        await expect(T.rowSlots(done, 'aka'), `${name}: and none for Aka`).toHaveCount(0);
        expect(await doneRowWinner(done), `${name}: Shiro won the bout`).toBe('shiro');
        await expect.poll(() => boutNames(done), { message: `${name}: the recorded bout names the lineup's fighters` }).toEqual(opener);
      }
      await shot(d1, 'f-kachinuki-recorded-device-1');
      await shot(d2, 'f-kachinuki-recorded-device-2');
      record({ journey: JOURNEY, case: 'f kachinuki', pair, gapMs, bout1: (await doneRows(d2, ROOT.d2).nth(0).innerText()).replace(/\s+/g, ' ') });
    } finally {
      await ops.close();
    }
  });

  test('g a tap answered as counted that a finish made offline elsewhere turns away is told on its device, naming the match and the tap', async ({ browser, baseURL }) => {
    const ops = await twoDevices(browser, baseURL, 'A', ids.individual);
    try {
      const { d1, d2, ctx1 } = ops;
      await expect.poll(() => inlineRunning(d1)).toBe(true);
      await ops.openD2();
      await sameMatchOnBoth(d1, d2);
      await expect.poll(() => marksOf(d2, 'aka', ROOT.d2)).toEqual(['K']);
      // Device 1, the court's tablet, goes offline, scores Shiro's second
      // point and finishes the match: both wait in its outbox. An offline
      // holder keeps running its shiaijo.
      await ctx1.setOffline(true);
      try {
        await tapOnSide(d1, 'shiro', ipponButton(d1, 'shiro', 'D'));
        await expect.poll(() => marksOf(d1, 'shiro')).toEqual(['D', 'M']);
        const armed = await armFinish(d1);
        await armed.tap();
        expect(await controlState(d1), 'device 1 still runs shiaijo A offline').toBe('holds');
        // Then device 2, online, scores for Aka from the Scores page: the
        // server counts it, and its own answer says so.
        await tapOnSide(d2, 'aka', ipponButton(d2, 'aka', 'D', ROOT.d2));
        await settled(d2);
        await expect.poll(() => marksOf(d2, 'aka', ROOT.d2), { message: 'device 2\'s tap is counted at first' }).toEqual(['D', 'K']);
      } finally {
        await ctx1.setOffline(false);
      }
      // Device 1's point and finish land: they are the court's (device 1 runs
      // the shiaijo), Shiro had won the bout first, and the match was finished
      // before device 2's tap. Device 2 is told, though its tap's own answer
      // said it counted.
      const told = await toastText(d2, /^Not applied in .+ \(Aka D added\): /);
      expect(told, 'the alert names the match and the tap').toMatch(/^Not applied in .+ \(Aka D added\): (the bout was already decided|the match is finished)/);
      await shot(d2, 'g-device-2-told');
      record({ journey: JOURNEY, case: 'g turned away later', told });
    } finally {
      await ops.close();
    }
  });

  test('h a second court console on the shiaijo watches until Take control, and the first takes it back the same way', async ({ browser, baseURL }) => {
    const ops = await twoDevices(browser, baseURL, 'A', ids.individual);
    const ctx3 = await browser.newContext({ ...OPERATOR_DEVICE, baseURL });
    try {
      const { d1 } = ops;
      const d3 = await ctx3.newPage();
      await login(d3);
      const opened = await openShiaijo(d3, 'A', { take: false });
      expect(opened.state, 'a console on another tablet does not take the shiaijo by itself').toBe('other');
      const watching = await controlText(d3);
      expect(watching, 'its bar names another device').toMatch(OTHER_DEVICE_TEXT('A'));
      expect(watching, 'and not another tab of its own tablet').not.toMatch(/another tab on this tablet/);
      // Case g's Finish started the court's next match (Finish + Start Next):
      // the watching console shows it running, and cannot score it or send it back.
      await expect.poll(() => inlineRunning(d3), { message: 'the watching console shows the running match' }).toBe(true);
      await expect(ipponButton(d3, 'shiro', 'M'), 'the watching console cannot score').toBeDisabled();
      await expect(d3.getByRole('button', { name: 'Send back to queue' }), 'nor send the match back').toBeDisabled();
      expect(await controlText(d1), 'device 1 still runs it').toBe(HOLDS_TEXT('A'));
      await expect(ipponButton(d1, 'shiro', 'M')).toBeEnabled();
      await shot(d3, 'h-second-console-watching');

      // Take control on the second tablet: one question, then the green bar.
      const asked = await takeControl(d3);
      expect(asked).toBe('The other device will stop running shiaijo A. Take control?');
      await expect.poll(() => controlState(d1), { message: 'device 1 learns it no longer runs shiaijo A' }).toBe('other');
      expect(await controlText(d1), 'device 1 shows the amber bar').toBe('Shiaijo A is now run from another device. Results this tablet still has queued count like any other device\'s.');
      await expect(ipponButton(d1, 'shiro', 'M'), 'device 1 can no longer score').toBeDisabled();
      await expect(ipponButton(d3, 'shiro', 'M'), 'the console that took control can').toBeEnabled();
      await shot(d1, 'h-device-1-lost-control');

      // A take by mistake is undone with the same two taps.
      await takeControl(d1);
      await expect.poll(() => controlState(d3), { message: 'the second console watches again' }).toBe('other');
      expect(await awaitControl(d1)).toBe('holds');
      await expect(ipponButton(d1, 'shiro', 'M'), 'device 1 scores again').toBeEnabled();
      record({ journey: JOURNEY, case: 'h second console', watching, asked });
    } finally {
      await ctx3.close();
      await ops.close();
    }
  });
});
