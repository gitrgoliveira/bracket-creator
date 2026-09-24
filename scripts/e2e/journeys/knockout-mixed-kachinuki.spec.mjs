// J5, kachinuki (winner stays on), played AS the distracted, clumsy court
// operator (mp-yqxn.2's PERSONA AUDIT).
//
// Fixture F5, seeded only through the interface: a team knockout, kachinuki,
// team size 5, four teams over shiaijo A and B. The draw puts semifinal M1 on
// A, M2 on B and the final M3 on A. Every team member is named through the
// court console's "Enter lineup" panel before its first match.
//
// The journey drives M1 from /admin/shiaijo/A eight bouts deep (wins, a
// hikiwake, an encho, a taisho tie that exhausts a team, a fusensho walkover),
// returns to a mis-scored earlier bout mid-encounter, then walks every
// correction door the UI offers: Remove this bout, Reopen match on a completed
// encounter, the court-busy "Clear its score, queue it, and reopen" panel
// (M2, moved onto A, holds the court), and the refusal when the final that M1
// feeds is already running. M2 ends the other way the rules allow, with the
// taisho drawing, staying on, and then being defeated. The final is played
// out and a spectator's phone sees the finished knockout.
//
// Functional defects found here are test.fixme('<bead-id>: ...') blocks,
// each a self-contained reproduction placed where the fixture supports it.
// Each was run once as a plain test() and failed at its own assertion. The
// tests after one tolerate what it leaves behind, so removing a fixme once
// its bead is fixed does not disturb them. Judgement findings, and behaviour
// the docs do not rule on (Send back to queue under a REOPENED encounter
// wiping its bout log: the docs promise the log survives a reopen, and that
// Send back to queue clears a running match's score), are recorded as audit
// rows and never asserted.
//
// Asserted: only the functional outcomes the docs promise (who stays on, the
// recorded result, the bracket advancing). The clumsy variants are PERFORMED
// through fixtures/clumsy.mjs and RECORDED as audit rows (AUDIT lines in the
// log, audit.json attached); whether an outcome is acceptable is judged from
// the screenshots in output/knockout-mixed-kachinuki/, never asserted.
import { test, expect } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { openShiaijo, sides, upNextCard } from '../fixtures/shiaijo.mjs';
import { PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { openViewer } from '../fixtures/public.mjs';
// clumsy.hastyConfirm answers confirmDialog (ui.jsx) only. The other confirms
// on this journey are the shiaijo page's own `.shiaijo-move-confirm`, the
// inline reason prompt and the inline court-busy panel, which it cannot see;
// the local helpers at the end of this file answer those the same way.
import { doubleTap, hastyConfirm, interrupt, retapWhileSaving, tapNeighbour } from '../fixtures/clumsy.mjs';
import {
  awardBoutIppon, boutFilled, ensureCoarse, boutIpponButton, boutMiddle, boutNames, closeLineupPanel, correctingBout,
  currentBout, doneRowWinner, doneRows, editor, enchoButton, endMatch, endMatchButton, lineupSide,
  openLineupFromUpNext, recordBout, recordBoutButton, removeBoutButton, reopenButton, syncPill, tieButton,
  typeLineup, winBout,
} from '../fixtures/kachinuki.mjs';

const JOURNEY = 'knockout-mixed-kachinuki';

// clumsy.interrupt, then put the coarse pointer back if the interruption lost
// it (see ensureCoarse), and say so in the audit row.
const interruptC = async (page, kind, opts) => ({ ...(await interrupt(page, kind, opts)), ...(await ensureCoarse(page)) });

const TEAMS = [
  ['Team Hayabusa', 'Kita Dojo'], ['Team Kawasemi', 'Minami Dojo'],
  ['Team Tsubame', 'Higashi Dojo'], ['Team Washi', 'Nishi Dojo'],
];
// Senpo to Taisho.
const MEMBERS = {
  'Team Hayabusa': ['Aoki', 'Baba', 'Chiba', 'Doi', 'Endo'],
  'Team Kawasemi': ['Fujii', 'Goto', 'Hara', 'Ishii', 'Jin'],
  'Team Tsubame': ['Kudo', 'Maeda', 'Noda', 'Ono', 'Sato'],
  'Team Washi': ['Taki', 'Ueno', 'Wada', 'Yano', 'Zaitsu'],
};

test.describe.configure({ mode: 'serial' });

test.describe('J5 kachinuki from the court console', () => {
  let shot;
  let compId;
  const audit = [];
  // One audit row: step | action | variant | what happened (observed facts).
  // The judgement columns are formed from the screenshots, not here.
  const record = (row) => {
    audit.push(row);
    console.log(`AUDIT ${JSON.stringify(row)}`);
  };
  // Pairings as the draw put them (read back, never assumed).
  let m1; // { shiro, aka } team names on M1 (court A)
  let m2; // on M2 (court B)
  let finalPair; // on M3, the final (court A)

  test.beforeAll(() => {
    shot = journeyShots(JOURNEY);
  });

  test.afterAll(async ({}, testInfo) => {
    await testInfo.attach('audit.json', { body: JSON.stringify(audit, null, 2), contentType: 'application/json' });
  });

  test('F5: seed a kachinuki knockout and name every team member at the court', async ({ page }) => {
    await createTournament(page, { name: 'Kachinuki Cup', courts: 2 });
    compId = await createCompetition(page, {
      name: 'Team Kachinuki', kind: 'team', format: 'knockout', teamSize: 5,
      teamMatchType: 'kachinuki', courts: ['A', 'B'], numberPrefix: 'T',
    });
    await pasteRoster(page, compId, TEAMS);
    await generateDraw(page, compId);
    await startCompetition(page, compId);

    for (const court of ['A', 'B']) {
      await openShiaijo(page, court);
      const pair = await sides(upNextCard(page));
      if (court === 'A') m1 = pair; else m2 = pair;
      await openLineupFromUpNext(page);
      if (court === 'B') {
        // V3: five names typed, the tab reloads before Save lineup.
        await typeLineup(page, pair.shiro, MEMBERS[pair.shiro], { save: false });
        const reload = await interruptC(page, 'reload');
        const panelBack = await page.getByRole('heading', { name: 'Lineup for this match' }).isVisible();
        if (!panelBack) await openLineupFromUpNext(page);
        const kept = await lineupSide(page, pair.shiro).locator('input.pmf__input').evaluateAll((els) => els.map((e) => e.value));
        record({ step: 'M2 lineup', action: 'type five names, not saved', variant: 'V3 interrupt reload', ...reload,
          panelStillOpen: panelBack, namesKept: kept, screenshot: await shot(page, 'v3-lineup-reload-unsaved') });
        await typeLineup(page, pair.shiro, MEMBERS[pair.shiro], { save: false });
        // V2: Save lineup double-tapped.
        const save = lineupSide(page, pair.shiro).getByRole('button', { name: 'Save lineup' });
        const v2 = await doubleTap(save);
        await expect(lineupSide(page, pair.shiro).getByRole('button', { name: 'Save lineup' })).toBeEnabled();
        await page.waitForTimeout(800);
        record({ step: 'M2 lineup', action: 'Save lineup', variant: 'V2 doubleTap', ...v2,
          toasts: await page.locator('.toast, [role="status"]').allInnerTexts().catch(() => []),
          saved: await lineupSide(page, pair.shiro).locator('input.pmf__input').evaluateAll((els) => els.map((e) => e.value)),
          screenshot: await shot(page, 'v2-lineup-save-double-tap') });
        await typeLineup(page, pair.aka, MEMBERS[pair.aka]);
      } else {
        for (const team of [pair.shiro, pair.aka]) await typeLineup(page, team, MEMBERS[team]);
      }
      await shot(page, `lineup-${court}-saved`);
      await closeLineupPanel(page);
    }
  });

  // Semifinal M1 on shiaijo A. The intended encounter (Shiro = m1.shiro):
  //   1  S1 v A1  Shiro wins          5  S4 v A3  Shiro wins (mis-scored M M, truth M K)
  //   2  S1 v A2  Aka wins            6  S4 v A4  Shiro wins
  //   3  S2 v A2  hikiwake            7  S4 v A5  hikiwake: Aka's taisho ties, Aka exhausted
  //   4  S3 v A3  encho, Aka wins     8  S5 v A5  fusensho to Shiro, then End match
  test('J5 M1: eight bouts from the court console, each action done clumsily too', async ({ page }) => {
    const S = MEMBERS[m1.shiro];
    const A = MEMBERS[m1.aka];
    const expectPairing = async (n, shiro, aka) => {
      await expect.poll(() => boutNames(currentBout(page)), { message: `bout ${n} pairing` }).toEqual({ shiro, aka });
      expect(await doneRows(page).count()).toBe(n - 1);
    };

    await test.step('open shiaijo A; Start match, with a neighbour tap and a double tap', async () => {
      // Each test() has its own browser context: sign in through the form.
      await login(page);
      await openShiaijo(page, 'A');
      const start = upNextCard(page).getByRole('button', { name: 'Start match' });
      const box = await start.boundingBox();
      // V1: the thumb lands beside Start match.
      const v1 = await tapNeighbour(start, 'right');
      const lineupOpened = await page.getByRole('heading', { name: 'Lineup for this match' })
        .waitFor({ state: 'visible', timeout: 3000 }).then(() => true, () => false);
      const v1Shot = await shot(page, 'v1-start-neighbour');
      if (lineupOpened) await closeLineupPanel(page);
      record({ step: 'M1 start', action: 'Start match (Up next card)', variant: 'V1 tapNeighbour right', ...v1,
        lineupOpened, startBox: box, screenshot: v1Shot });
      // V2: two taps inside 300ms on Start match.
      const v2 = await doubleTap(start);
      await expect(editor(page)).toBeVisible();
      await expect(recordBoutButton(page)).toBeVisible();
      const v2Shot = await shot(page, 'v2-start-double-tap');
      record({ step: 'M1 start', action: 'Start match (Up next card)', variant: 'V2 doubleTap', ...v2,
        eyebrow: (await editor(page).locator('.editor-modal__eyebrow').first().innerText()).trim(),
        running: await recordBoutButton(page).isVisible(), screenshot: v2Shot });
      await expectPairing(1, S[0], A[0]);
      await shot(page, 'bout-1-open');
    });

    await test.step('bout 1: Shiro wins M K; a double tap, a neighbour tap, a reload before Record', async () => {
      const row = currentBout(page);
      // V2 on an ippon: two taps inside 300ms on Shiro's M.
      const v2 = await doubleTap(boutIpponButton(row, 'shiro', 'M'));
      await page.waitForTimeout(300);
      const marks = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
      const v2Shot = await shot(page, 'v2-ippon-double-tap');
      // Recover: tap the second mark (the editor's "Tap a scored mark to clear it").
      let recoverTaps = 0;
      while ((await boutFilled(currentBout(page), 'shiro').count()) > 1) {
        await boutFilled(currentBout(page), 'shiro').last().tap();
        recoverTaps += 1;
      }
      record({ step: 'M1 bout 1', action: 'Shiro M', variant: 'V2 doubleTap', ...v2, marksAfter: marks,
        recoverTaps, hintShown: await editor(page).getByTestId('team-scoring-clear-hint').isVisible(), screenshot: v2Shot });
      // V1: the thumb lands on the button beside M.
      const v1 = await tapNeighbour(boutIpponButton(currentBout(page), 'shiro', 'M'), 'right');
      await page.waitForTimeout(300);
      const after = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
      record({ step: 'M1 bout 1', action: 'Shiro M (second point)', variant: 'V1 tapNeighbour right', ...v1, marksAfter: after,
        screenshot: await shot(page, 'v1-ippon-neighbour') });
      // Put the board where the operator meant it: M K for Shiro.
      while ((await boutFilled(currentBout(page), 'shiro').count()) > 1) await boutFilled(currentBout(page), 'shiro').last().tap();
      if ((await boutFilled(currentBout(page), 'shiro').count()) === 0) await awardBoutIppon(page, 'shiro', 'M');
      await awardBoutIppon(page, 'shiro', 'K');
      // V3: the iPad reloads the tab before Record bout was tapped.
      await expect(syncPill(page)).toHaveText('Synced').catch(() => {});
      const reload = await interruptC(page, 'reload');
      await expect(editor(page)).toBeVisible();
      const kept = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
      record({ step: 'M1 bout 1', action: 'scored bout, not yet recorded', variant: 'V3 interrupt reload', ...reload,
        marksKept: kept, doneRows: await doneRows(page).count(), pairing: await boutNames(currentBout(page)),
        screenshot: await shot(page, 'v3-reload-bout-1') });
      // V2 on Record bout: a re-tap while it reads "Saving…".
      const retap = await retapWhileSaving(recordBoutButton(page));
      await page.waitForTimeout(800);
      record({ step: 'M1 bout 1', action: 'Record bout', variant: 'V2 retapWhileSaving', ...retap,
        doneRows: await doneRows(page).count(), current: await boutNames(currentBout(page)),
        screenshot: await shot(page, 'v2-record-retap') });
      await expectPairing(2, S[0], A[1]);
      expect(await doneRowWinner(doneRows(page).nth(0))).toBe('shiro');
    });

    await test.step('bout 2: Aka wins; a neighbour tap on Record bout arms End match', async () => {
      await awardBoutIppon(page, 'aka', 'M');
      await awardBoutIppon(page, 'aka', 'D');
      const v1 = await tapNeighbour(recordBoutButton(page), 'right');
      await page.waitForTimeout(300);
      const endLabel = (await endMatchButton(page).innerText()).trim();
      record({ step: 'M1 bout 2', action: 'Record bout', variant: 'V1 tapNeighbour right', ...v1, endLabel,
        screenshot: await shot(page, 'v1-record-neighbour-end-armed') });
      await recordBout(page);
      await expectPairing(3, S[1], A[1]);
      expect(await doneRowWinner(doneRows(page).nth(1))).toBe('aka');
    });

    await test.step('bout 3: the appended pairing removed by mistake, then a double tap on Tie', async () => {
      // "× Remove this bout" sits under the unscored appended pairing. The
      // hurried thumb double-taps it.
      const remove = removeBoutButton(page);
      await expect(remove).toBeVisible();
      const removeBox = await remove.boundingBox();
      const v2 = await doubleTap(remove);
      await page.waitForTimeout(1200);
      const afterRemove = { doneRows: await doneRows(page).count(), current: await boutNames(currentBout(page)),
        currentShiroMarks: await boutFilled(currentBout(page), 'shiro').count(), currentAkaMarks: await boutFilled(currentBout(page), 'aka').count() };
      const removeShot = await shot(page, 'v2-remove-bout-double-tap');
      // Recover: Record bout again re-appends the same pairing.
      await recordBout(page);
      record({ step: 'M1 bout 3', action: '× Remove this bout', variant: 'V2 doubleTap', ...v2, removeBox, afterRemove,
        recoveredTo: await boutNames(currentBout(page)), recoverTaps: 1, screenshot: removeShot });
      await expectPairing(3, S[1], A[1]);

      const tie = tieButton(currentBout(page));
      const v2tie = await doubleTap(tie);
      await page.waitForTimeout(300);
      const tieLabel = (await tie.innerText()).trim();
      record({ step: 'M1 bout 3', action: 'Tie (hikiwake)', variant: 'V2 doubleTap', ...v2tie, tieLabel,
        middle: await boutMiddle(currentBout(page)), screenshot: await shot(page, 'v2-tie-double-tap') });
      if (!/✓/.test(tieLabel)) await tie.tap();
      await expect(tie).toHaveText(/✓ Tie/);
      const v1tie = await tapNeighbour(tie, 'up');
      await page.waitForTimeout(300);
      record({ step: 'M1 bout 3', action: 'Tie (hikiwake)', variant: 'V1 tapNeighbour up', ...v1tie,
        tieStill: (await tie.innerText()).trim(), middle: await boutMiddle(currentBout(page)),
        screenshot: await shot(page, 'v1-tie-neighbour') });
      if (!/✓/.test((await tie.innerText()).trim())) await tie.tap();
      await expect(tie).toHaveText(/✓ Tie/);
      await recordBout(page);
      // A tie retires both: the next pair comes up.
      await expectPairing(4, S[2], A[2]);
      expect(await boutMiddle(doneRows(page).nth(2))).toBe('X');
    });

    await test.step('bout 4: a tie fought on in encho, Aka scores; double tap on Encho, tab hidden mid-encho', async () => {
      await tieButton(currentBout(page)).tap();
      await expect(enchoButton(page)).toBeVisible();
      await shot(page, 'bout-4-tied-encho-offered');
      const v2 = await doubleTap(enchoButton(page));
      await page.waitForTimeout(300);
      const eyebrow = (await editor(page).locator('.editor-modal__eyebrow').first().innerText()).trim();
      record({ step: 'M1 bout 4', action: 'Encho', variant: 'V2 doubleTap', ...v2, eyebrow,
        enchoStillOffered: await enchoButton(page).isVisible(), middle: await boutMiddle(currentBout(page)),
        screenshot: await shot(page, 'v2-encho-double-tap') });
      const hidden = await interruptC(page, 'hidden');
      record({ step: 'M1 bout 4', action: 'encho running', variant: 'V3 interrupt hidden', ...hidden,
        pairing: await boutNames(currentBout(page)), middle: await boutMiddle(currentBout(page)),
        screenshot: await shot(page, 'v3-hidden-encho') });
      await awardBoutIppon(page, 'aka', 'K');
      await recordBout(page);
      await expectPairing(5, S[3], A[2]);
      expect(await boutMiddle(doneRows(page).nth(3))).toBe('(E)');
      expect(await doneRowWinner(doneRows(page).nth(3))).toBe('aka');
    });

    await test.step('bout 5: the hurried double tap records M M for a win that was M K', async () => {
      await doubleTap(boutIpponButton(currentBout(page), 'shiro', 'M'));
      await expect(boutFilled(currentBout(page), 'shiro')).toHaveCount(2);
      await recordBout(page);
      await expectPairing(6, S[3], A[3]);
    });

    await test.step('bout 6: a point tapped offline, and the Back button mid-bout', async () => {
      let whileOffline;
      const offline = await interruptC(page, 'offline', {
        during: async (p) => {
          await boutIpponButton(currentBout(p), 'shiro', 'M').tap();
          await p.waitForTimeout(1500);
          whileOffline = (await syncPill(p).innerText().catch(() => '?')).trim();
        },
      });
      const resynced = await expect(syncPill(page)).toHaveText('Synced', { timeout: 10_000 }).then(() => true, () => false);
      record({ step: 'M1 bout 6', action: 'Shiro M', variant: 'V3 interrupt offline', ...offline, whileOffline, resynced,
        marks: await boutFilled(currentBout(page), 'shiro').allInnerTexts(), screenshot: await shot(page, 'v3-offline-bout-6') });
      const back = await interruptC(page, 'back');
      await expect(editor(page)).toBeVisible();
      record({ step: 'M1 bout 6', action: 'bout with one point', variant: 'V3 interrupt back', ...back,
        pairing: await boutNames(currentBout(page)), marks: await boutFilled(currentBout(page), 'shiro').allInnerTexts(),
        doneRows: await doneRows(page).count(), screenshot: await shot(page, 'v3-back-bout-6') });
      await awardBoutIppon(page, 'shiro', 'K');
      await recordBout(page);
      await expectPairing(7, S[3], A[4]);
      await shot(page, 'bout-7-open-seven-deep');
      // Harness defect, measured here: a fullPage screenshot leaves the page at
      // a FINE pointer afterwards, so the coarse tap floors switch off. This
      // journey takes fullPage shots only where a reload is harmless (or one
      // follows anyway) and restores the coarse pointer straight after.
      const coarseBeforeFull = await page.evaluate(() => matchMedia('(pointer: coarse)').matches);
      await shot(page, 'bout-7-open-seven-deep-full', { fullPage: true });
      const coarseAfterFull = await page.evaluate(() => matchMedia('(pointer: coarse)').matches);
      record({ step: 'harness', action: 'fullPage screenshot', variant: 'measurement', coarseBeforeFull, coarseAfterFull,
        ...(await ensureCoarse(page)) });
      // V5 lost place: the operator comes back to the iPad at the top of the
      // page. What does the first screen show seven bouts in?
      await page.evaluate(() => window.scrollTo(0, 0));
      const inView = async (loc) => {
        const b = await loc.boundingBox();
        return !!b && b.y >= 0 && b.y + b.height <= 820;
      };
      record({ step: 'M1 bout 7', action: 'glance at the sheet seven bouts in (scrolled to top)', variant: 'V5 lost place',
        liveBoutNamesInView: await inView(currentBout(page).locator('input.pmf__input').first()),
        recordBoutInView: await inView(recordBoutButton(page)), endMatchInView: await inView(endMatchButton(page)),
        runningTotalShown: await editor(page).locator('.team-summary').count(),
        screenshot: await shot(page, 'v5-seven-deep-scrolled-top') });
      // Tap targets on the live bout, measured under the coarse pointer.
      const row = currentBout(page);
      const sizes = {};
      const measure = async (name, loc) => {
        const b = await loc.first().boundingBox();
        sizes[name] = b ? `${Math.round(b.width)}x${Math.round(b.height)}` : 'not rendered';
      };
      await measure('ippon M', boutIpponButton(row, 'shiro', 'M'));
      await measure('Fusensho', row.getByTestId('scoring-modal-fusensho-button'));
      await measure('Tie (hikiwake)', tieButton(row));
      await measure('foul +', row.getByRole('button', { name: /^Add a .* foul$/ }));
      await measure('empty mark slot', row.locator('.tsm-center-pts--shiro .editor-side__pt'));
      await measure('Record bout', recordBoutButton(page));
      await measure('End match', endMatchButton(page));
      await measure('× Remove this bout', removeBoutButton(page));
      await measure('+ Add next bout manually', editor(page).getByTestId('kachinuki-add-bout-button'));
      await measure('fought bout row', doneRows(page).nth(0));
      await measure('fighter name box', row.locator('.lineup-name__bar'));
      await measure('clear fighter ×', row.locator('.lineup-name__clear'));
      await measure('Send back to queue', page.getByRole('button', { name: 'Send back to queue' }));
      const probe = await tieButton(row).evaluate((el) => ({
        coarse: matchMedia('(pointer: coarse)').matches,
        tieMinHeight: getComputedStyle(el).minHeight,
        tieHeight: getComputedStyle(el).height,
      }));
      record({ step: 'M1 bout 7', action: 'measure tap targets (coarse pointer, floor 44px)', variant: 'measurement', sizes, ...probe });
    });

    await test.step('return to mis-scored bout 5 mid-encounter and fix M M to M K', async () => {
      const row5 = doneRows(page).nth(4);
      expect(await boutNames(row5)).toEqual({ shiro: S[3], aka: A[2] });
      const marksOf = async (i) => ({
        shiro: await editor(page).locator('.team-sub-match').nth(i).locator('.tsm-center-pts--shiro .editor-side__pt--filled').allInnerTexts(),
        aka: await editor(page).locator('.team-sub-match').nth(i).locator('.tsm-center-pts--aka .editor-side__pt--filled').allInnerTexts(),
      });
      // V1: the thumb meant bout 5 and landed on the row below it.
      const v1 = await tapNeighbour(row5, 'down');
      await page.waitForTimeout(400);
      const openedWrong = await correctingBout(page).count() ? await boutNames(correctingBout(page)) : null;
      const v1Shot = await shot(page, 'v1-fought-row-neighbour');
      let v1Recover = 0;
      for (const caretEl of await editor(page).locator('[data-testid^="kachinuki-done-collapse-"]').all()) {
        await caretEl.tap();
        v1Recover += 1;
      }
      record({ step: 'M1 correction', action: 'tap fought bout 5 to correct it', variant: 'V1 tapNeighbour down', ...v1,
        openedForCorrection: openedWrong, recoverTaps: v1Recover, screenshot: v1Shot });
      // V2: two taps inside 300ms on the fought row. The first expands it; where
      // does the second land?
      const before = await marksOf(4);
      const v2 = await doubleTap(doneRows(page).nth(4));
      await page.waitForTimeout(600);
      // Which bout did the double tap leave open?
      const openCaret = editor(page).locator('[data-testid^="kachinuki-done-collapse-"]');
      const openIdx = await openCaret.count()
        ? Number((await openCaret.first().getAttribute('data-testid')).split('-').pop()) : -1;
      record({ step: 'M1 correction', action: 'tap fought bout 5 to correct it', variant: 'V2 doubleTap', ...v2,
        boutOpenAfter: openIdx + 1, bout5MarksBefore: before, bout5MarksAfter: await marksOf(4),
        screenshot: await shot(page, 'v2-fought-row-double-tap') });
      // Recover: collapse whatever opened, then one deliberate tap on bout 5.
      if (openIdx >= 0 && openIdx !== 4) await openCaret.first().tap();
      if (!(await editor(page).getByTestId('kachinuki-done-collapse-4').count())) {
        await expect(correctingBout(page)).toHaveCount(0);
        await doneRows(page).nth(4).tap();
      }
      const corr = correctingBout(page);
      await expect(editor(page).getByTestId('kachinuki-done-collapse-4')).toBeVisible();
      expect(await boutFilled(corr, 'shiro').allInnerTexts()).toEqual(['M', 'M']);
      await shot(page, 'bout-5-open-for-correction');
      const caret = await editor(page).getByTestId('kachinuki-done-collapse-4').boundingBox();
      record({ step: 'M1 correction', action: 'measure the collapse triangle', variant: 'measurement',
        caret: caret ? `${Math.round(caret.width)}x${Math.round(caret.height)}` : 'not rendered' });
      await corr.locator('.tsm-center-pts--shiro .editor-side__pt--filled').last().tap();
      await expect(boutFilled(corr, 'shiro')).toHaveCount(1);
      await boutIpponButton(corr, 'shiro', 'K').tap();
      await expect(boutFilled(corr, 'shiro')).toHaveText(['M', 'K']);
      const warn = await editor(page).getByTestId('kachinuki-done-edit-warn').isVisible();
      await shot(page, 'bout-5-corrected-open');
      // V5: which bout is live while a past one is open? Both carry buttons.
      record({ step: 'M1 correction', action: 'tap fought bout 5, fix M M to M K', variant: 'correct path',
        winnerFlipWarning: warn, liveBout: await boutNames(currentBout(page)),
        editableRows: await editor(page).locator('.team-sub-match:not(.team-sub-match--readonly)').count(),
        screenshot: await shot(page, 'bout-5-and-live-bout-both-editable', { fullPage: true }) });
      // V3: the Back button while bout 5 is open for correction.
      await expect(syncPill(page)).toHaveText('Synced');
      const back = await interruptC(page, 'back');
      await expect(editor(page)).toBeVisible();
      await page.waitForTimeout(500);
      record({ step: 'M1 correction', action: 'bout 5 open for correction', variant: 'V3 interrupt back', ...back,
        stillOpen: await correctingBout(page).count(), bout5: await boutNames(doneRows(page).nth(4)).catch(() => null),
        bout5Marks: await marksOf(4), live: await boutNames(currentBout(page)), screenshot: await shot(page, 'v3-back-mid-correction') });
      if (!(await correctingBout(page).count())) await doneRows(page).nth(4).tap();
      // V2 on the collapse triangle.
      const caretBtn = editor(page).getByTestId('kachinuki-done-collapse-4');
      const v2c = await doubleTap(caretBtn);
      await page.waitForTimeout(400);
      const reopened = await correctingBout(page).count();
      record({ step: 'M1 correction', action: 'collapse triangle', variant: 'V2 doubleTap', ...v2c,
        openAfter: reopened, screenshot: await shot(page, 'v2-collapse-double-tap') });
      if (reopened) await editor(page).getByTestId('kachinuki-done-collapse-4').tap();
      await expect(correctingBout(page)).toHaveCount(0);
      await expect(syncPill(page)).toHaveText('Synced');
      expect(await boutFilled(doneRows(page).nth(4), 'shiro').allInnerTexts()).toEqual(['M', 'K']);
      // The live bout is untouched by the correction.
      await expectPairing(7, S[3], A[4]);
    });

    await test.step('a correction that flips who won an earlier bout: the warning, then put back', async () => {
      const row1 = doneRows(page).nth(0);
      await row1.tap();
      const corr = correctingBout(page);
      await expect(corr).toBeVisible();
      // Clear Shiro's two marks, give Aka two: bout 1 now reads as Aka's.
      while ((await boutFilled(corr, 'shiro').count()) > 0) await boutFilled(corr, 'shiro').last().tap();
      await boutIpponButton(corr, 'aka', 'M').tap();
      await boutIpponButton(corr, 'aka', 'M').tap();
      const warning = editor(page).getByTestId('kachinuki-done-edit-warn');
      const warned = await warning.isVisible();
      const warningText = warned ? (await warning.innerText()).trim() : '';
      const flipShot = await shot(page, 'bout-1-winner-flipped-warning');
      const laterUnchanged = await boutNames(currentBout(page));
      // Put it back as it was fought.
      while ((await boutFilled(corr, 'aka').count()) > 0) await boutFilled(corr, 'aka').last().tap();
      await boutIpponButton(corr, 'shiro', 'M').tap();
      await boutIpponButton(corr, 'shiro', 'K').tap();
      await editor(page).getByTestId('kachinuki-done-collapse-0').tap();
      await expect(syncPill(page)).toHaveText('Synced');
      record({ step: 'M1 correction', action: 'flip bout 1 winner by correction, then restore', variant: 'correct path',
        warned, warningText, liveBoutAfterFlip: laterUnchanged,
        screenshot: flipShot });
      expect(await doneRowWinner(doneRows(page).nth(0))).toBe('shiro');
      await expectPairing(7, S[3], A[4]);
    });

    await test.step("bout 7: Aka's taisho ties and Aka is out of fighters", async () => {
      await tieButton(currentBout(page)).tap();
      await shot(page, 'bout-7-taisho-tie');
      await recordBout(page);
      // The app keeps the fighter who just tied and brings up Shiro's next.
      await expectPairing(8, S[4], A[4]);
    });

    await test.step('bout 8: fusensho to Shiro (exhaustion walkover), then End match double-tapped', async () => {
      const fus = currentBout(page).locator('.team-sub-match__side--shiro').getByTestId('scoring-modal-fusensho-button');
      // V1: the thumb meant Fusensho and landed on the button beside it.
      const v1 = await tapNeighbour(fus, 'left');
      await page.waitForTimeout(300);
      const marks = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
      const v1Shot = await shot(page, 'v1-fusensho-neighbour');
      let recoverTaps = 0;
      while ((await boutFilled(currentBout(page), 'shiro').count()) > 0) {
        await boutFilled(currentBout(page), 'shiro').last().tap();
        recoverTaps += 1;
      }
      record({ step: 'M1 bout 8', action: 'Fusensho (Shiro)', variant: 'V1 tapNeighbour left', ...v1, shiroMarks: marks,
        recoverTaps, screenshot: v1Shot });
      // V2: Fusensho double-tapped (it is a toggle).
      const v2 = await doubleTap(fus);
      await page.waitForTimeout(300);
      const label = (await fus.innerText()).trim();
      record({ step: 'M1 bout 8', action: 'Fusensho (Shiro)', variant: 'V2 doubleTap', ...v2, labelAfter: label,
        shiroMarks: await boutFilled(currentBout(page), 'shiro').allInnerTexts(), screenshot: await shot(page, 'v2-fusensho-double-tap') });
      if (!/✓/.test(label)) await fus.tap();
      await expect(fus).toHaveText(/✓ Fusensho/);
      await shot(page, 'bout-8-fusensho');
      // V2 on End match: two taps inside 300ms on a two-tap guard.
      const end = endMatchButton(page);
      const endBox = await end.boundingBox();
      const v2end = await doubleTap(end);
      const completed = await page.locator('.shiaijo-completed .shiaijo-qrow')
        .filter({ hasText: m1.shiro }).filter({ hasText: m1.aka }).first()
        .waitFor({ state: 'visible', timeout: 8000 }).then(() => true, () => false);
      const endLabelAfter = completed ? null : (await end.innerText().catch(() => null));
      record({ step: 'M1 end', action: 'End match (two-tap guard)', variant: 'V2 doubleTap', ...v2end, endBox,
        completedByDoubleTap: completed, endLabelAfter, screenshot: await shot(page, 'v2-end-match-double-tap') });
      if (!completed) {
        if (/^Tap again/.test(endLabelAfter || '')) await end.tap(); else await endMatch(page);
      }
      const row = completedRow(page, m1);
      await expect(row).toBeVisible();
      // Shiro won bouts 1, 5, 6 and 8 (the walkover's two maru), Aka 2 and 4:
      // IV 4-2, PW 8-3. Does the court's own Completed record say so?
      const want = /IV 4–2\s+PW 8–3/;
      const result = row.locator('.shiaijo-qrow__result');
      const settled = await expect(result).toHaveText(want, { timeout: 6000 }).then(() => true, () => false);
      const shownAtFirst = (await result.innerText()).replace(/\s+/g, ' ');
      const staleShot = await shot(page, 'm1-completed');
      await page.getByRole('button', { name: 'Refresh' }).tap();
      await page.waitForTimeout(1500);
      record({ step: 'M1 end', action: 'Completed row after End match', variant: 'result', expected: 'IV 4–2 PW 8–3',
        shownFor6s: shownAtFirst, settledWithoutRefresh: settled,
        afterRefresh: (await completedRow(page, m1).locator('.shiaijo-qrow__result').innerText()).replace(/\s+/g, ' '),
        screenshot: staleShot });
      // The bracket advanced: the final on A now names M1's winner (Shiro).
      await expect(page.locator('.shiaijo-pending, .shiaijo-upnext').first()).toContainText(m1.shiro);
    });
  });

  // The completed M1 turns out to be wrong: bout 8 was not a walkover, Jin
  // fought and won by men. Correct (Completed list) -> Reopen match -> fix the
  // bout -> End match, which now asks for a reason.
  test('J5 M1 reopened from Completed: fix bout 8, end it again', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');

    await test.step('Correct on the completed row opens the finished encounter', async () => {
      // V1: is there anything beside Correct for the thumb to land on?
      const v1 = await tapNeighbour(completedRow(page, m1).locator('.shiaijo-row__correct'), 'left')
        .catch((e) => ({ none: e.message.split('\n')[0] }));
      await page.waitForTimeout(300);
      const cb = await completedRow(page, m1).locator('.shiaijo-row__correct').boundingBox();
      record({ step: 'M1 reopen', action: 'Correct (Completed row)', variant: 'V1 tapNeighbour left', ...v1,
        correctBox: cb && `${Math.round(cb.width)}x${Math.round(cb.height)}`, screenshot: await shot(page, 'v1-correct-neighbour') });
      await completedRow(page, m1).locator('.shiaijo-row__correct').tap();
      await expect(reopenButton(page)).toBeVisible();
      const rows = editor(page).locator('.team-sub-match');
      record({ step: 'M1 reopen', action: 'Correct (Completed row)', variant: 'correct path',
        boutRowsShown: await rows.count(),
        band: (await editor(page).locator('.team-summary').first().innerText().catch(() => '')).replace(/\s+/g, ' ').trim(),
        screenshot: await shot(page, 'm1-correct-completed-view') });
      // V1 on a completed kachinuki sheet: the thumb lands on an ippon button of
      // a finished bout. There is no Save correction on this view.
      const firstRow = rows.nth(0);
      const before = await boutFilled(firstRow, 'aka').count();
      const btn = boutIpponButton(firstRow, 'aka', 'M');
      const enabled = await btn.isEnabled().catch(() => false);
      if (enabled) await btn.tap();
      await page.waitForTimeout(400);
      record({ step: 'M1 reopen', action: 'tap an ippon on the completed sheet', variant: 'V1 stray tap',
        buttonEnabled: enabled, akaMarksBefore: before, akaMarksAfter: await boutFilled(firstRow, 'aka').count(),
        saveButton: await editor(page).getByRole('button', { name: /Save correction/ }).count(),
        screenshot: await shot(page, 'v1-stray-tap-completed-sheet') });
    });

    await test.step('Reopen match, double-tapped', async () => {
      const reopen = reopenButton(page);
      const box = await reopen.boundingBox();
      const v2 = await doubleTap(reopen);
      await expect(recordBoutButton(page)).toBeVisible({ timeout: 15_000 });
      await page.waitForTimeout(500);
      record({ step: 'M1 reopen', action: 'Reopen match', variant: 'V2 doubleTap', ...v2, reopenBox: box,
        error: (await page.getByTestId('kachinuki-reopen-error').innerText().catch(() => '')).trim(),
        doneRows: await doneRows(page).count(), current: await boutNames(currentBout(page)),
        screenshot: await shot(page, 'v2-reopen-double-tap') });
    });

    await test.step('reload while reopened: back on the same encounter?', async () => {
      const reload = await interruptC(page, 'reload');
      const back = await editor(page).waitFor({ state: 'visible', timeout: 15_000 }).then(() => true, () => false);
      record({ step: 'M1 reopen', action: 'reopened encounter', variant: 'V3 interrupt reload', ...reload, editorBack: back,
        eyebrow: back ? (await editor(page).locator('.editor-modal__eyebrow').first().innerText()).trim() : null,
        doneRows: back ? await doneRows(page).count() : null, current: back ? await boutNames(currentBout(page)) : null,
        screenshot: await shot(page, 'v3-reload-reopened') });
      expect(back).toBe(true);
    });

    // Undo the walkover on bout 8 and award the men Jin really scored.
    const fixBout8 = async () => {
      const fus = currentBout(page).locator('.team-sub-match__side--shiro').getByTestId('scoring-modal-fusensho-button');
      const wasFusensho = /✓/.test(await fus.innerText());
      if (wasFusensho) await fus.tap(); // re-tap undoes the walkover
      await expect(fus).toHaveText(/^Fusensho$/);
      while ((await boutFilled(currentBout(page), 'shiro').count()) > 0) await boutFilled(currentBout(page), 'shiro').last().tap();
      await awardBoutIppon(page, 'shiro', 'M');
      return wasFusensho;
    };
    const bout8Marks = () => boutFilled(currentBout(page), 'shiro').allInnerTexts();

    await test.step('fix bout 8: the walkover was really a men by Jin', async () => {
      // Bout 8 is the last bout; while reopened it is the live row.
      expect(await boutNames(currentBout(page))).toEqual({ shiro: MEMBERS[m1.shiro][4], aka: MEMBERS[m1.aka][4] });
      const wasFusensho = await fixBout8();
      await expect(syncPill(page)).toHaveText('Synced');
      record({ step: 'M1 reopen', action: 'undo fusensho, award M on bout 8', variant: 'correct path', wasFusensho,
        pill: (await syncPill(page).innerText()).trim(), screenshot: await shot(page, 'm1-bout-8-rescored') });
      // V3: two seconds later (well past the autosave's 300ms), the tab reloads.
      await page.waitForTimeout(2000);
      const reload = await interruptC(page, 'reload');
      await expect(recordBoutButton(page)).toBeVisible();
      const kept = await bout8Marks();
      const fusAfter = (await currentBout(page).locator('.team-sub-match__side--shiro')
        .getByTestId('scoring-modal-fusensho-button').innerText()).trim();
      record({ step: 'M1 reopen', action: 'bout 8 corrected on the live row, 2s idle', variant: 'V3 interrupt reload', ...reload,
        bout8MarksAfterReload: kept, fusenshoAfterReload: fusAfter, screenshot: await shot(page, 'v3-reload-after-bout-8-fix') });
      correctionState.autosaved = kept.length === 1 && kept[0] === 'M';
      if (!correctionState.autosaved) await fixBout8();
    });

    await test.step('End match on a reopened encounter asks for a reason; answered hastily', async () => {
      await endMatchButton(page).tap();
      await expect(page.locator('.reason-prompt')).toBeVisible();
      await shot(page, 'm1-reopen-reason-prompt');
      // V3: the tab reloads with the reason prompt open.
      const reload = await interruptC(page, 'reload');
      await expect(editor(page)).toBeVisible();
      record({ step: 'M1 reopen', action: 'reason prompt open', variant: 'V3 interrupt reload', ...reload,
        promptStill: await page.locator('.reason-prompt').isVisible(), stillRunning: await recordBoutButton(page).isVisible(),
        bout8: await boutNames(currentBout(page)), bout8Marks: await boutFilled(currentBout(page), 'shiro').allInnerTexts(),
        screenshot: await shot(page, 'v3-reload-reason-prompt') });
      // Whatever the reload brought back is what End would record: put the
      // correction back if it was lost.
      if ((await bout8Marks()).join('') !== 'M') await fixBout8();
      if (!(await page.locator('.reason-prompt').isVisible())) await endMatchButton(page).tap();
      const hasty = await hastyReasonPrompt(page);
      record({ step: 'M1 reopen', action: 'End match -> reason prompt', variant: 'V4 hasty confirm', ...hasty,
        screenshot: await shot(page, 'v4-reason-hasty') });
      const row = completedRow(page, m1);
      await expect(row).toBeVisible();
      await expect(row.locator('.shiaijo-qrow__result')).toContainText('IV');
      const immediately = (await row.locator('.shiaijo-qrow__result').innerText()).replace(/\s+/g, ' ');
      const immediateShot = await shot(page, 'm1-ended-again');
      // Bout 8 is now a men (1 point) instead of the walkover (2 maru).
      const settled = await expect(row.locator('.shiaijo-qrow__result')).toHaveText(/IV 4–2\s+PW 7–3/, { timeout: 8000 })
        .then(() => true, () => false);
      record({ step: 'M1 reopen', action: 'ended again: Completed row', variant: 'result', expected: 'IV 4–2 PW 7–3',
        immediately, settledWithin8s: settled, after: (await row.locator('.shiaijo-qrow__result').innerText()).replace(/\s+/g, ' '),
        screenshot: immediateShot });
      // Still Kawasemi's (Shiro's) win: the final keeps its side.
      await expect(page.locator('.shiaijo-pending, .shiaijo-upnext').first()).toContainText(m1.shiro);
    });
  });

  // The operator on A spots another mistake in M1, but the organiser has
  // already moved M2 onto A (shiaijo B's table is unstaffed) and it is running
  // there. Reopening M1 meets the court-busy panel.
  test('J5 M1 reopened while M2 holds shiaijo A: the court-busy panel', async ({ page }) => {
    await login(page);

    await test.step('the organiser moves M2 to shiaijo A from the Scores tab', async () => {
      await page.goto(`/admin/competition/${compId}/scores`);
      const row = page.locator('.score-edit-row').filter({ hasText: m2.shiro }).filter({ hasText: m2.aka }).first();
      await expect(row).toBeVisible();
      await row.locator('.score-edit-row__court--btn').tap();
      await page.getByRole('option', { name: 'A' }).tap();
      const confirm = page.locator('.shiaijo-move-confirm, .modal[role="dialog"]').filter({ hasText: /Move/ }).first();
      if (await confirm.isVisible().catch(() => false)) {
        await confirm.getByRole('button', { name: /^Move/ }).tap();
      }
      await expect(row.locator('.score-edit-row__court--btn')).toHaveText(/^A\b/);
      await shot(page, 'm2-moved-to-A');
    });

    await test.step('M2 starts on A and its first point is scored', async () => {
      await openShiaijo(page, 'A');
      expect(await sides(upNextCard(page))).toEqual(m2);
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      await expect(recordBoutButton(page)).toBeVisible();
      await awardBoutIppon(page, 'aka', 'M');
      await expect(syncPill(page)).toHaveText('Synced');
    });

    const conflict = page.getByTestId('kachinuki-reopen-conflict');
    await test.step('Correct M1 -> Reopen match: the court is busy', async () => {
      await completedRow(page, m1).locator('.shiaijo-row__correct').tap();
      await expect(reopenButton(page)).toBeVisible();
      await reopenButton(page).tap();
      await expect(conflict).toBeVisible();
      const req = await page.getByTestId('kachinuki-reopen-requeue-button').boundingBox();
      const leave = await page.getByTestId('kachinuki-reopen-conflict-dismiss').boundingBox();
      record({ step: 'M1 reopen, court busy', action: 'Reopen match while M2 runs on A', variant: 'correct path',
        panel: (await conflict.innerText()).replace(/\s+/g, ' ').trim(),
        requeueBox: req && `${Math.round(req.width)}x${Math.round(req.height)}`,
        leaveBox: leave && `${Math.round(leave.width)}x${Math.round(leave.height)}`,
        screenshot: await shot(page, 'm1-reopen-court-busy') });
    });

    await test.step('V1: the thumb meant the danger button and hit Leave it running', async () => {
      const v1 = await tapNeighbour(page.getByTestId('kachinuki-reopen-requeue-button'), 'right');
      await page.waitForTimeout(300);
      const panelStill = await conflict.isVisible();
      record({ step: 'M1 reopen, court busy', action: 'Clear its score, queue it, and reopen', variant: 'V1 tapNeighbour right',
        ...v1, panelStill, screenshot: await shot(page, 'v1-requeue-neighbour') });
      if (!panelStill) {
        await reopenButton(page).tap();
        await expect(conflict).toBeVisible();
      }
    });

    await test.step('V4: the panel answered by its loudest button without reading', async () => {
      const loud = await hastyPanel(conflict);
      await expect(conflict).toHaveCount(0, { timeout: 15_000 });
      await expect(recordBoutButton(page)).toBeVisible({ timeout: 15_000 });
      const upNext = await upNextCard(page).isVisible() ? await sides(upNextCard(page)) : null;
      record({ step: 'M1 reopen, court busy', action: 'court-busy panel', variant: 'V4 hasty (loudest button)', ...loud,
        m1Reopened: await boutNames(currentBout(page)), upNext,
        screenshot: await shot(page, 'v4-requeue-hasty') });
      // M1 is back in play with its bouts; M2 is back in A's queue.
      expect(await doneRows(page).count()).toBe(7);
      expect(upNext).toEqual(m2);
    });

    await test.step('M1 ended again (reason owed); M2 restarts with its score cleared', async () => {
      await endMatchButton(page).tap();
      await hastyReasonPrompt(page);
      await expect(completedRow(page, m1)).toBeVisible();
      // The operator carries straight on: Start match on the Up next card.
      await expect(upNextCard(page)).toBeVisible();
      expect(await sides(upNextCard(page))).toEqual(m2);
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      await page.waitForTimeout(1500);
      const afterStart = {
        upNextStill: await upNextCard(page).isVisible(),
        panelShows: (await editor(page).locator('.editor-modal__eyebrow').first().innerText().catch(() => '')).replace(/\s+/g, ' '),
        correctionBadge: await editor(page).getByText('Correction', { exact: true }).count(),
        m2Live: await recordBoutButton(page).isVisible(),
        backToCourt: await page.getByRole('button', { name: /Back to court/ }).count(),
      };
      record({ step: 'M1 reopen, court busy', action: 'End M1 again, then Start match on Up next (M2)', variant: 'V5 lost place',
        ...afterStart, screenshot: await shot(page, 'v5-m2-started-but-not-shown') });
      // Recover: "← Back to court" under the pinned correction.
      let recoverTaps = 0;
      if (!afterStart.m2Live && afterStart.backToCourt) {
        await page.getByRole('button', { name: /Back to court/ }).tap();
        recoverTaps += 1;
      }
      await expect(recordBoutButton(page)).toBeVisible();
      record({ step: 'M1 reopen, court busy', action: 'recover the started M2', variant: 'V5 lost place', recoverTaps,
        live: await boutNames(currentBout(page)), screenshot: await shot(page, 'v5-m2-after-back-to-court') });
      const marks = await boutFilled(currentBout(page), 'aka').count();
      record({ step: 'M1 reopen, court busy', action: 'M2 restarted after the requeue', variant: 'result',
        m2Bout1AkaMarks: marks, docsSay: 'Sending a match back to the queue clears any score already entered for it',
        screenshot: await shot(page, 'm2-restarted-on-A') });
      expect(marks).toBe(0);
    });
  });

  // M2, now on shiaijo A, ends the other way the rules allow: Aka's senpo wins
  // four in a row, Shiro's taisho draws him and stays on, and Aka's jiho then
  // defeats the taisho.
  test('J5 M2: the taisho draws, stays on, and is defeated', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');
    const S = MEMBERS[m2.shiro];
    const A = MEMBERS[m2.aka];
    const expectPairing = async (n, shiro, aka) => {
      await expect.poll(() => boutNames(currentBout(page)), { message: `M2 bout ${n} pairing` }).toEqual({ shiro, aka });
      expect(await doneRows(page).count()).toBe(n - 1);
    };
    await expect(recordBoutButton(page)).toBeVisible();

    await test.step('Aka senpo wins four bouts', async () => {
      for (let i = 0; i < 4; i += 1) {
        await expectPairing(i + 1, S[i], A[0]);
        if (i === 1) {
          // The fighter pick on a later bout (it rides the bout, not the lineup).
          // The list drops down OVER this side's ippon buttons.
          const box = () => currentBout(page).locator('.team-sub-match__side--shiro input.pmf__input');
          const option = (name) => currentBout(page).locator('.team-sub-match__side--shiro .pmf__option').filter({ hasText: name }).first();
          const shiroMarks = () => boutFilled(currentBout(page), 'shiro').allInnerTexts();
          // Open the list the way a thumb does. A box that already has focus
          // does not reopen its list on a second tap, so tap away first.
          const openList = async () => {
            let taps = 0;
            await box().tap(); taps += 1;
            if (!(await currentBout(page).locator('.team-sub-match__side--shiro .pmf__dropdown').isVisible())) {
              await currentBout(page).locator('.team-sub-match__pos').tap(); taps += 1;
              await box().tap(); taps += 1;
            }
            await expect(option(S[1])).toBeVisible();
            return taps;
          };
          const clearGhostMarks = async () => {
            let taps = 0;
            while ((await boutFilled(currentBout(page), 'shiro').count()) > 0) {
              await boutFilled(currentBout(page), 'shiro').last().tap();
              taps += 1;
            }
            return taps;
          };
          // Correct path: one tap on the right fighter.
          await openList();
          await shot(page, 'm2-fighter-pick-open');
          const optBox = await option(S[1]).boundingBox();
          await option(S[1]).tap();
          await page.waitForTimeout(600);
          const afterPick = await shiroMarks();
          const pickShot = await shot(page, 'm2-fighter-picked-one-tap');
          record({ step: 'M2 bout 2', action: 'pick the Shiro fighter (one tap)', variant: 'correct path',
            pairing: await boutNames(currentBout(page)), shiroMarksAfterPick: afterPick,
            optionBox: optBox && `${Math.round(optBox.width)}x${Math.round(optBox.height)}`,
            recoverTaps: await clearGhostMarks(), screenshot: pickShot });
          pickState.ghost = afterPick.length > 0;
          // V2: the option double-tapped.
          const reopenTaps = await openList();
          const v2 = await doubleTap(option(S[1]));
          await page.waitForTimeout(600);
          const v2Marks = await shiroMarks();
          const v2Shot = await shot(page, 'm2-v2-fighter-pick-double-tap');
          record({ step: 'M2 bout 2', action: 'pick the Shiro fighter', variant: 'V2 doubleTap', ...v2, tapsToReopenList: reopenTaps,
            pairing: await boutNames(currentBout(page)), shiroMarks: v2Marks, recoverTaps: await clearGhostMarks(), screenshot: v2Shot });
          // A laptop operator closes the fighter list with Esc (the sheet's
          // own shortcut line says "Esc close").
          await openList();
          await page.keyboard.press('Escape');
          await page.waitForTimeout(400);
          const discard = page.locator('.modal[role="dialog"]').filter({ hasText: /Discard unsaved/ });
          const dialogShown = await discard.isVisible();
          const escShot = await shot(page, 'm2-esc-discard-dialog');
          let hasty = {};
          if (dialogShown) {
            hasty = await hastyConfirm(page);
            await page.waitForTimeout(800);
          }
          record({ step: 'M2 bout 2', action: 'Esc to close the fighter list', variant: 'V4 hastyConfirm', dialogShown, ...hasty,
            editorStill: await recordBoutButton(page).isVisible(), pairing: await boutNames(currentBout(page)).catch(() => null),
            listStillOpen: await currentBout(page).locator('.team-sub-match__side--shiro .pmf__dropdown').isVisible(),
            pill: (await syncPill(page).innerText()).trim(),
            doneRows: await doneRows(page).count(), screenshot: escShot, after: await shot(page, 'm2-after-esc-discard') });
          // V1: the option below is hit instead.
          await openList();
          const v1 = await tapNeighbour(option(S[1]), 'down');
          await page.waitForTimeout(600);
          const wrong = await boutNames(currentBout(page));
          const v1Marks = await shiroMarks();
          const v1Shot = await shot(page, 'm2-v1-fighter-pick-neighbour');
          // Recover: clear any mark, pick the right fighter again.
          let recoverTaps = await clearGhostMarks();
          if (wrong.shiro !== S[1]) {
            recoverTaps += await openList();
            await option(S[1]).tap();
            recoverTaps += 1;
            await page.waitForTimeout(600);
            recoverTaps += await clearGhostMarks();
          }
          await expect.poll(() => boutNames(currentBout(page))).toEqual({ shiro: S[1], aka: A[0] });
          record({ step: 'M2 bout 2', action: 'pick the Shiro fighter', variant: 'V1 tapNeighbour down', ...v1,
            pairingAfterMistap: wrong, shiroMarks: v1Marks, recoverTaps, screenshot: v1Shot });
          expect(await boutFilled(currentBout(page), 'shiro').count()).toBe(0);
          await winBout(page, 'aka');
        } else if (i === 2) {
          // V3 mid-bout: the tab goes to the background with one point on the board.
          await awardBoutIppon(page, 'aka', 'M');
          const hidden = await interruptC(page, 'hidden', { awayMs: 1500 });
          record({ step: 'M2 bout 3', action: 'bout with one point', variant: 'V3 interrupt hidden', ...hidden,
            marks: await boutFilled(currentBout(page), 'aka').allInnerTexts(), pairing: await boutNames(currentBout(page)),
            screenshot: await shot(page, 'm2-v3-hidden') });
          await awardBoutIppon(page, 'aka', 'K');
          await recordBout(page);
        } else if (i === 3) {
          await awardBoutIppon(page, 'aka', 'M');
          await awardBoutIppon(page, 'aka', 'M');
          // V2 on Record bout: two taps inside 300ms.
          const before = await doneRows(page).count();
          const v2 = await doubleTap(recordBoutButton(page));
          await expect(doneRows(page)).toHaveCount(before + 1);
          await page.waitForTimeout(1200);
          record({ step: 'M2 bout 4', action: 'Record bout', variant: 'V2 doubleTap', ...v2,
            doneRows: await doneRows(page).count(), current: await boutNames(currentBout(page)),
            recordEnabled: await recordBoutButton(page).isEnabled(), screenshot: await shot(page, 'm2-v2-record-double-tap') });
        } else {
          await winBout(page, 'aka');
        }
      }
    });

    await test.step("Shiro's taisho draws: he stays on against Aka's jiho", async () => {
      await expectPairing(5, S[4], A[0]);
      await tieButton(currentBout(page)).tap();
      await recordBout(page);
      await expectPairing(6, S[4], A[1]);
      await shot(page, 'm2-taisho-stays-on');
    });

    await test.step('Aka jiho defeats the taisho; End match, two taps', async () => {
      await awardBoutIppon(page, 'aka', 'D');
      await awardBoutIppon(page, 'aka', 'M');
      await endMatchButton(page).tap();
      await expect(endMatchButton(page)).toHaveText(/^Tap again/);
      const armed = (await endMatchButton(page).innerText()).trim();
      const b = await endMatchButton(page).boundingBox();
      record({ step: 'M2 end', action: 'End match (first tap arms)', variant: 'correct path', armed,
        endBox: b && `${Math.round(b.width)}x${Math.round(b.height)}`, screenshot: await shot(page, 'm2-end-armed') });
      await endMatchButton(page).tap();
      await expect(completedRow(page, m2)).toBeVisible();
      await shot(page, 'm2-completed');
    });

    await test.step('the hurried operator reopens M2 and taps Send back to queue to get out', async () => {
      await completedRow(page, m2).locator('.shiaijo-row__correct').tap();
      await reopenButton(page).tap();
      await expect(recordBoutButton(page)).toBeVisible({ timeout: 15_000 });
      const boutsBefore = await doneRows(page).count();
      const revert = page.getByRole('button', { name: 'Send back to queue' });
      await expect(revert).toBeVisible();
      await revert.tap();
      await shot(page, 'm2-send-back-confirm');
      const hasty = await hastyShiaijoConfirm(page);
      await page.waitForTimeout(1500);
      // Where did the encounter go, and did its bout log survive?
      const upNext = await upNextCard(page).isVisible() ? await sides(upNextCard(page)).catch(() => null) : null;
      let boutsAfter = null;
      if (upNext) {
        await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
        await expect(recordBoutButton(page)).toBeVisible();
        await page.waitForTimeout(800);
        boutsAfter = await doneRows(page).count();
      }
      record({ step: 'M2 reopened', action: 'Send back to queue (reopened encounter)', variant: 'V4 hasty confirm', ...hasty,
        boutsBefore, upNext, boutsAfterRestart: boutsAfter, current: upNext ? await boutNames(currentBout(page)) : null,
        screenshot: await shot(page, 'm2-after-send-back') });
      m2State.boutsAfter = boutsAfter;
    });

    await test.step('M2 is entered again from the paper sheet and ended', async () => {
      // If the bout log survived, only End match is owed. If it did not, the
      // operator re-enters the encounter from scratch.
      if (m2State.boutsAfter === 0) {
        for (let i = 0; i < 4; i += 1) await winBout(page, 'aka');
        await tieButton(currentBout(page)).tap();
        await recordBout(page);
        await awardBoutIppon(page, 'aka', 'D');
        await awardBoutIppon(page, 'aka', 'M');
      }
      await endMatchButton(page).tap();
      if (await page.locator('.reason-prompt').isVisible()) await hastyReasonPrompt(page);
      else await endMatchButton(page).tap();
      await expect(completedRow(page, m2)).toBeVisible();
      await shot(page, 'm2-completed-again');
    });
  });

  // bc-crpn: while a finished match is pinned open for correction, Start
  // match on the Up next card starts the next match but the panel keeps
  // showing the correction: the running match appears nowhere on the page.
  test.fixme('bc-crpn: Start match while a correction is pinned hides the match it started', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');
    const next = await sides(upNextCard(page));
    await completedRow(page, m1).locator('.shiaijo-row__correct').tap();
    await expect(reopenButton(page)).toBeVisible();
    await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
    await expect(recordBoutButton(page)).toBeVisible();
    expect(await editor(page).locator('.team-sub-match__side--shiro input.pmf__input').first().inputValue())
      .toBe(MEMBERS[next.shiro][0]);
    // Leave the final as it was: back in the queue, unscored.
    await page.getByRole('button', { name: 'Send back to queue' }).tap();
    await page.locator('.shiaijo-move-confirm').getByRole('button', { name: 'Send back to queue' }).tap();
    await expect(upNextCard(page)).toBeVisible();
  });

  test('J5 final: lineups copied, the final starts on A', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');
    const final = await sides(upNextCard(page));
    // The final pairs the two semifinal winners: M1's Shiro and M2's Aka.
    expect([final.shiro, final.aka].sort()).toEqual([m1.shiro, m2.aka].sort());
    finalPair = final;

    await test.step('lineups for the final: Copy from previous match, double-tapped', async () => {
      await openLineupFromUpNext(page);
      const sidesEls = page.locator('[data-testid^="match-lineup-side-"]');
      const copy = sidesEls.nth(0).getByRole('button', { name: /Copy from previous match|Copying…/ });
      const v2 = await doubleTap(copy);
      await page.waitForTimeout(1500);
      const first = await sidesEls.nth(0).locator('input.pmf__input').evaluateAll((els) => els.map((e) => e.value));
      record({ step: 'final lineup', action: 'Copy from previous match', variant: 'V2 doubleTap', ...v2, lineup: first,
        screenshot: await shot(page, 'final-copy-lineup-double-tap') });
      await sidesEls.nth(1).getByRole('button', { name: 'Copy from previous match' }).tap();
      await page.waitForTimeout(1500);
      await shot(page, 'final-lineups-copied');
      await closeLineupPanel(page);
    });

    await test.step('start the final and score one point', async () => {
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      await expect(recordBoutButton(page)).toBeVisible();
      expect(await boutNames(currentBout(page))).toEqual({ shiro: MEMBERS[final.shiro][0], aka: MEMBERS[final.aka][0] });
      await awardBoutIppon(page, 'shiro', 'M');
      await expect(syncPill(page)).toHaveText('Synced');
      await shot(page, 'final-running-one-point');
    });

    await test.step('Correct M1 -> Reopen while the final (fed by M1) runs', async () => {
      await completedRow(page, m1).locator('.shiaijo-row__correct').tap();
      await expect(reopenButton(page)).toBeVisible();
      await reopenButton(page).tap();
      const err = page.getByTestId('kachinuki-reopen-error');
      await expect(err).toBeVisible();
      record({ step: 'final running', action: 'Reopen M1 while its downstream final runs on the same court', variant: 'correct path',
        message: (await err.innerText()).trim(), courtBusyPanel: await page.getByTestId('kachinuki-reopen-conflict').count(),
        screenshot: await shot(page, 'm1-reopen-refused-downstream') });
      // Back to the live final.
      await page.getByRole('button', { name: /Back to court/ }).tap();
      await expect(recordBoutButton(page)).toBeVisible();
    });
  });

  // bc-emsl: a tap on an EMPTY mark slot (the "·" beside the scored
  // marks) is not a no-op: it clears the bout's Tie.
  test.fixme('bc-emsl: tapping an empty mark slot clears the bout\'s Tie', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');
    const row = currentBout(page);
    // The final's live bout carries one Shiro point; clear it, mark the tie.
    while ((await boutFilled(row, 'shiro').count()) > 0) await boutFilled(row, 'shiro').last().tap();
    await tieButton(row).tap();
    await expect(tieButton(row)).toHaveText(/✓ Tie/);
    await row.locator('.tsm-center-pts--aka .editor-side__pt:not(.editor-side__pt--filled)').first().tap();
    await expect(tieButton(row)).toHaveText(/✓ Tie/);
    await expect(boutMiddle(row)).resolves.toBe('X');
    // Restore: untie, one Shiro point.
    await tieButton(row).tap();
    await awardBoutIppon(page, 'shiro', 'M');
  });

  // bc-kheb: Encho on ONE kachinuki bout sets the encounter's overtime
  // count, so the header keeps reading "(E) OVERTIME" on every later bout.
  test.fixme('bc-kheb: after one bout goes to encho, the header claims overtime on every later bout', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');
    const row = currentBout(page);
    while ((await boutFilled(row, 'shiro').count()) > 0) await boutFilled(row, 'shiro').last().tap();
    await tieButton(row).tap();
    await enchoButton(page).tap();
    await awardBoutIppon(page, 'shiro', 'M');
    await recordBout(page);
    // Bout 2 is a regulation bout.
    await expect(editor(page).locator('.editor-modal__eyebrow').first()).not.toContainText(/overtime/i);
  });

  // bc-sync: for the autosave's debounce window after a tap, the sync pill
  // already reads "Synced" although nothing has been sent; a reload in that
  // window loses the point with no warning (audit row "final bout 4").
  test.fixme('bc-sync: the sync pill reads "Synced" while a tapped point is still unsaved', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');
    const live = currentBout(page);
    await boutIpponButton(live, 'aka', 'D').tap();
    await page.waitForTimeout(150);
    await expect(syncPill(page)).not.toHaveText('Synced', { timeout: 100 });
    // Leave the bout as it was: take the point back.
    await expect(syncPill(page)).toHaveText('Synced');
    await boutFilled(currentBout(page), 'aka').last().tap();
    await expect(boutFilled(currentBout(page), 'aka')).toHaveCount(0);
  });

  test('J5 final: played out, the knockout completes', async ({ page, browser, baseURL }) => {
    await login(page);
    await openShiaijo(page, 'A');
    await expect(recordBoutButton(page)).toBeVisible();
    const S = MEMBERS[finalPair.shiro];
    // Shiro keeps winning with whoever is on until End match; the operator
    // ends it when Aka is out of fighters. The Aka fighter changes each bout.
    for (let i = 0; i < 5; i += 1) {
      const live = currentBout(page);
      if (i === 3) {
        // V3: the tab reloads a moment after a point is tapped. What does the
        // sync pill say in that moment, and does the point survive?
        await boutIpponButton(live, 'shiro', 'M').tap();
        await page.waitForTimeout(150);
        const pillAtTap = (await syncPill(page).innerText()).trim();
        const reload = await interruptC(page, 'reload');
        await expect(recordBoutButton(page)).toBeVisible();
        const kept = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
        record({ step: 'final bout 4', action: 'Shiro M, then the tab reloads 150ms later', variant: 'V3 interrupt reload', ...reload,
          pillAt150ms: pillAtTap, marksAfterReload: kept, screenshot: await shot(page, 'final-v3-reload-150ms-after-tap') });
      }
      if ((await boutFilled(currentBout(page), 'shiro').count()) === 0) await awardBoutIppon(page, 'shiro', 'M');
      await awardBoutIppon(page, 'shiro', 'K');
      if (i === 1) {
        // V3: the network drops just as Record bout is tapped.
        const before = await doneRows(page).count();
        let whileOffline;
        const offline = await interruptC(page, 'offline', {
          awayMs: 2500,
          during: async (p) => {
            await recordBoutButton(p).tap();
            await p.waitForTimeout(1500);
            whileOffline = {
              doneRows: await doneRows(p).count(),
              banner: (await editor(p).locator('.pending-write-banner').allInnerTexts()).join(' | '),
              recordLabel: (await recordBoutButton(p).innerText().catch(() => '')).trim(),
              live: await boutNames(currentBout(p)).catch(() => null),
            };
            await shot(p, 'final-v3-record-offline');
          },
        });
        const appended = await expect(doneRows(page)).toHaveCount(before + 1, { timeout: 15_000 }).then(() => true, () => false);
        record({ step: 'final bout 2', action: 'Record bout', variant: 'V3 interrupt offline', ...offline, whileOffline,
          appendedAfterReconnect: appended, doneRows: await doneRows(page).count(), live: await boutNames(currentBout(page)),
          screenshot: await shot(page, 'final-after-reconnect') });
        if (!appended) await recordBout(page);
      } else if (i < 4) {
        await recordBout(page);
      }
    }
    expect((await boutNames(currentBout(page))).shiro).toBe(S[0]);
    await endMatch(page);
    const done = page.locator('.shiaijo-completed .shiaijo-qrow').filter({ hasText: finalPair.shiro }).filter({ hasText: finalPair.aka });
    await expect(done).toBeVisible();
    await shot(page, 'final-completed', { fullPage: true });

    // A spectator's phone: the finished knockout on the public viewer.
    const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    try {
      const phone = await spectator.newPage();
      await openViewer(phone, compId);
      await expect(phone.locator('.viewer__body')).toContainText(finalPair.shiro);
      await shot(phone, 'viewer-knockout-complete', { fullPage: true });
    } finally {
      await spectator.close();
    }
  });
});

const m2State = {};
const pickState = {};
const correctionState = {};

// A completed-list row naming both teams of a pairing.
function completedRow(page, pair) {
  return page.locator('.shiaijo-completed .shiaijo-qrow').filter({ hasText: pair.shiro }).filter({ hasText: pair.aka }).first();
}

// V4 for the shiaijo page's own confirms (`.shiaijo-move-confirm`: Send back
// to queue, Move to Shiaijo X). They are not confirmDialog (ui.jsx), so
// clumsy.hastyConfirm cannot see them; same rule: tap the loudest button.
async function hastyShiaijoConfirm(page) {
  const dialog = page.locator('.shiaijo-move-confirm');
  await dialog.waitFor({ state: 'visible' });
  const r = await loudestIn(dialog.locator('.shiaijo-move-confirm__actions'));
  const title = (await dialog.locator('.shiaijo-move-confirm__title').innerText()).trim();
  const message = (await dialog.locator('.shiaijo-move-confirm__body').innerText()).trim();
  await r.tap();
  await dialog.waitFor({ state: 'hidden', timeout: 15_000 });
  return { label: r.label, prominence: r.prominence, otherLabels: r.others, title, message };
}

// V4 for the inline reason prompt (ReasonPrompt): its loudest button, with
// whatever preset it offered by default.
async function hastyReasonPrompt(page) {
  const prompt = page.locator('.reason-prompt');
  await prompt.waitFor({ state: 'visible' });
  const preset = await prompt.locator('select').first().inputValue().catch(() => '');
  const r = await loudestIn(prompt);
  await r.tap();
  await prompt.waitFor({ state: 'hidden', timeout: 15_000 });
  return { label: r.label, prominence: r.prominence, otherLabels: r.others, defaultReason: preset };
}

// V4 for an inline panel (the court-busy conflict): its loudest button.
async function hastyPanel(panel) {
  const r = await loudestIn(panel);
  await r.tap();
  return { label: r.label, prominence: r.prominence, otherLabels: r.others };
}

// The most prominent button in `scope`: danger over primary over plain over
// ghost, the larger breaking a tie (the same ranking as clumsy.hastyConfirm).
async function loudestIn(scope) {
  const buttons = scope.locator('button');
  const rank = (cls) => (/\bbtn--danger\b/.test(cls) ? 3 : /\bbtn--primary\b/.test(cls) ? 2 : /\bbtn--ghost\b/.test(cls) ? 0 : 1);
  const PROMINENCE = ['ghost', 'plain', 'primary', 'danger'];
  let best = null;
  const labels = [];
  for (let i = 0; i < await buttons.count(); i += 1) {
    const b = buttons.nth(i);
    if (!(await b.isVisible())) continue;
    const cls = (await b.getAttribute('class')) || '';
    const box = await b.boundingBox();
    const label = (await b.innerText()).trim();
    labels.push(label);
    const score = [rank(cls), box ? box.width * box.height : 0];
    if (!best || score[0] > best.score[0] || (score[0] === best.score[0] && score[1] > best.score[1])) best = { b, label, score };
  }
  if (!best) throw new Error('no visible button to answer hastily');
  return {
    label: best.label, prominence: PROMINENCE[best.score[0]], others: labels.filter((l) => l !== best.label),
    tap: () => best.b.tap(),
  };
}
