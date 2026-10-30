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
// encounter, the court console refusing Correct while another bout is live on
// its court (bc-crpn), and from the Scores tab the court-busy "Queue it and
// reopen" panel (M2, moved onto A, holds the court) and the refusal when the
// final that M1 feeds is already running. M2 ends the other way the rules
// allow, with the taisho drawing, staying on, and then being defeated. The
// final is played out and a spectator's phone sees the finished knockout.
//
// Functional defects found here were filed as beads and kept as tests named
// after them ('<bead-id>: ...'), each a self-contained reproduction placed
// where the fixture supports it: first as test.fixme, and, once the bead was
// fixed, as plain tests that guard the fix. The tests after one tolerate what
// it leaves behind. Judgement findings, and Send back to queue under a
// REOPENED encounter (since ruled by bc-sbq: it keeps the bout log and the
// score), are recorded as audit rows and never asserted.
//
// Asserted: only the functional outcomes the docs promise (who stays on, the
// recorded result, the bracket advancing). The clumsy variants are PERFORMED
// through fixtures/clumsy.mjs and RECORDED as audit rows (AUDIT lines in the
// log, and the journey folder's audit.json, fixtures/audit.mjs); whether an
// outcome is acceptable is judged from the screenshots in the same folder
// (fixtures/shots.mjs), never asserted.
import { test, expect } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { completedRowFor, openShiaijo, sides, upNextCard } from '../fixtures/shiaijo.mjs';
import { PUBLIC_DEVICE, EXPECT_TIMEOUT_MS } from '../fixtures/devices.mjs';
import { openViewer, recentResult } from '../fixtures/public.mjs';
import { scoreRows } from '../fixtures/scores.mjs';
import { nextMatchWrite, settled, unsentCount, writeLanded } from '../fixtures/scoring.mjs';
import { EDITOR } from '../../screenshots/lib/editor.mjs';
import { dwell, dwellOnSide, rendered, settled as paceSettled, tapOnSide, untilStable } from '../fixtures/pace.mjs';
import { ensureCoarse } from '../fixtures/pointer.mjs';
// clumsy.hastyConfirm answers confirmDialog (ui.jsx) and hastyShiaijoConfirm
// the shiaijo page's own `.shiaijo-move-confirm`. The inline reason prompt and
// the inline court-busy panel are neither; the local helpers at the end of
// this file answer those by the same ranking (clumsy.loudestButton).
import {
  doubleTap, hastyConfirm, hastyShiaijoConfirm, interrupt, loudestButton, retapWhileSaving, tapNeighbour,
} from '../fixtures/clumsy.mjs';
import { closeNameList } from '../fixtures/team.mjs';
import {
  BOUT_CHANGE, awardBoutIppon, boutFilled, clearBoutMarks, boutIpponButton, boutMiddle, boutNames, closeLineupPanel, correctingBout,
  currentBout, doneRowWinner, doneRows, editor, enchoButton, enchoUndoButton, endMatch, endMatchButton, lineupSide, lineupSource,
  openLineupFromUpNext, recordBout, recordBoutButton, removeBoutButton, reopenButton, tieButton,
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
  // One audit row: step | action | variant | what happened (observed facts).
  // The judgement columns are formed from the screenshots, not here.
  const audit = auditLog(JOURNEY);
  const { record } = audit;
  // Pairings as the draw put them (read back, never assumed).
  let m1; // { shiro, aka } team names on M1 (court A)
  let m2; // on M2 (court B)
  let finalPair; // on M3, the final (court A)

  test.beforeAll(() => {
    shot = journeyShots(JOURNEY);
  });

  test.afterAll(() => audit.flush());

  // The four regression guards below (bc-crpn, bc-emsl, bc-kheb, bc-sync) need
  // the final's live console, so they run as steps INSIDE the final's tests. A
  // failure is deferred, not thrown: it is recorded, the console is put back,
  // and the final's own checks (played out, completed, the public viewer) still
  // run; the played-out test fails at its very end naming every guard that did.
  // (In a serial file a failing test skips the rest, and a restarted worker
  // loses the shared state, so a guard of its own could not stand before them.)
  const guardFailures = [];
  async function guarded(page, title, fn) {
    try {
      await test.step(title, () => fn(page));
    } catch (e) {
      const message = String(e.message).split('\n')[0];
      // Recorded at the moment it happens: the guard's name stays visible even
      // when a later step fails first.
      test.info().annotations.push({ type: 'guard-failure', description: `${title}: ${message}` });
      guardFailures.push(`${title}: ${message}`);
      record({ step: title, action: 'regression guard', variant: 'FAILED', error: message });
      // Cleanup is best effort: its own error must not replace the guard's failure.
      try {
        await openShiaijo(page, 'A').catch(() => {});
        if (await recordBoutButton(page).isVisible().catch(() => false)) {
          // Leave the live bout unmarked and untied, as the final's play-out expects.
          const live = currentBout(page);
          for (const side of ['shiro', 'aka']) await clearBoutMarks(live, side);
          if (/✓ Tie/.test(await tieButton(live).innerText().catch(() => ''))) await tieButton(live).tap();
        }
      } catch (cleanupError) {
        record({ step: title, action: 'regression guard cleanup', variant: 'FAILED', error: String(cleanupError.message).split('\n')[0] });
      }
    }
  }

  // bc-crpn (fixed): the court console never holds a running match and an
  // open correction at once. While a finished match is open for correction,
  // Start match on the Up next card is refused with a notice at the card, the
  // correction stays open and the queued match is not started.
  async function guardCrpn(page) {
    const next = await sides(upNextCard(page));
    await completedRowFor(page, m1).locator('.shiaijo-row__correct').tap();
    await expect(reopenButton(page)).toBeVisible();
    const start = upNextCard(page).getByRole('button', { name: 'Start match' });
    await start.tap();
    const notice = upNextCard(page).locator('.shiaijo-upnext__error');
    await expect(notice).toHaveText(new RegExp(
      `^Save the correction of .*${m1.shiro} vs ${m1.aka}, or leave it with Back to court, then start this match\\.$`));
    record({ step: 'bc-crpn', action: 'Start match (Up next) while M1 is open for correction', variant: 'refused',
      notice: (await notice.innerText()).trim(),
      toast: (await page.locator('.toast').allInnerTexts().catch(() => [])).join(' | '),
      screenshot: await shot(page, 'bc-crpn-start-refused-during-correction') });
    // The correction stays open, and the final is still waiting in the queue.
    await expect(reopenButton(page)).toBeVisible();
    await expect(recordBoutButton(page)).toHaveCount(0);
    await expect(start).toBeVisible();
    expect(await sides(upNextCard(page))).toEqual(next);
    // Leave the correction; the court is as it was, with the final up next.
    await page.getByRole('button', { name: /Back to court/ }).tap();
    await expect(reopenButton(page)).toHaveCount(0);
    await expect(notice).toHaveCount(0);
    expect(await sides(upNextCard(page))).toEqual(next);
  }

  // bc-emsl (fixed): a tap on an EMPTY mark slot (the "·" beside the scored
  // marks) used to clear the bout's Tie. It is a no-op now; this guards it.
  async function guardEmsl(page) {
    const row = currentBout(page);
    // The final's live bout carries one Shiro point; clear it, mark the tie.
    await clearBoutMarks(row, 'shiro');
    await tieButton(row).tap();
    await expect(tieButton(row)).toHaveText(/✓ Tie/);
    await row.locator('.tsm-center-pts--aka .editor-side__pt:not(.editor-side__pt--filled)').first().tap();
    await expect(tieButton(row)).toHaveText(/✓ Tie/);
    await expect(boutMiddle(row)).resolves.toBe('X');
    // Restore: untie, one Shiro point.
    await tieButton(row).tap();
    await awardBoutIppon(page, 'shiro', 'M');
  }

  // bc-kheb (fixed): Encho on ONE kachinuki bout used to set the encounter's
  // overtime count, so the header kept reading "(E) OVERTIME" on every later
  // bout. The (E) now lives on that bout's row only; this guards it.
  async function guardKheb(page) {
    const row = currentBout(page);
    await clearBoutMarks(row, 'shiro');
    await tieButton(row).tap();
    await enchoButton(page).tap();
    await awardBoutIppon(page, 'shiro', 'M');
    const bout1 = await doneRows(page).count();
    await recordBout(page);
    // Positive check first: Encho was recorded on bout 1 (its row's centre reads (E)).
    expect(await boutMiddle(doneRows(page).nth(bout1))).toBe('(E)');
    // Bout 2 has rendered (the live row is a fresh, unmarked one) before the negative check.
    await expect(currentBout(page)).toBeVisible();
    await expect(boutFilled(currentBout(page), 'shiro')).toHaveCount(0);
    // Bout 2 is a regulation bout.
    await expect(editor(page).locator('.editor-modal__eyebrow').first()).not.toContainText(/overtime/i);
  }

  // bc-sync (fixed): a tapped point still unsent survives a reload.
  // No editor autosaves now: a tap is an outbox entry from the moment it
  // is made and goes to the score-events door (bc-evts), and stays unsent
  // until the server has answered it; this guards it.
  // The tap's batch is held, so the outbox is checked while the point is provably
  // still unsaved, whatever this machine's timing.
  async function guardSync(page) {
    const live = currentBout(page);
    const isTapBatch = (url) => /\/api\/competitions\/[^/]+\/matches\/[^/]+\/score-events$/.test(url.pathname);
    let release;
    const released = new Promise((r) => { release = r; });
    const hold = async (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      await released;
      return route.continue().catch(() => {});
    };
    await page.route(isTapBatch, hold);
    try {
      const sent = page.waitForRequest((r) => r.method() === 'POST' && isTapBatch(new URL(r.url())));
      // A test that fails before the tap goes out must not leave this rejecting unheard.
      sent.catch(() => {});
      await boutIpponButton(live, 'aka', 'D').tap();
      // From the tap: the point is in the outbox...
      await expect.poll(() => unsentCount(page)).toBeGreaterThan(0);
      // ...then its batch is out, and held: still unsaved.
      await sent;
      await expect.poll(() => unsentCount(page)).toBeGreaterThan(0);
    } finally {
      release();
      await page.unroute(isTapBatch, hold);
    }
    await settled(page);
    // Leave the bout as it was: take the point back.
    await boutFilled(currentBout(page), 'aka').last().tap();
    await expect(boutFilled(currentBout(page), 'aka')).toHaveCount(0);
  }

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
        // The typed names come back as a draft kept in this tab, and the side says
        // so once the lineup has been read; a notice that never shows is recorded as none.
        const notice = lineupSide(page, pair.shiro).locator('[data-testid^="match-lineup-draft-"]');
        await notice.waitFor({ state: 'visible', timeout: 3000 }).catch(() => {});
        const kept = await lineupSide(page, pair.shiro).locator('input.pmf__input').evaluateAll((els) => els.map((e) => e.value));
        const draftNotice = (await notice.innerText({ timeout: 1000 }).catch(() => '')).replace(/\s+/g, ' ').trim();
        record({ step: 'M2 lineup', action: 'type five names, not saved', variant: 'V3 interrupt reload', ...reload,
          panelStillOpen: panelBack, namesKept: kept, draftNotice, screenshot: await shot(page, 'v3-lineup-reload-unsaved') });
        await typeLineup(page, pair.shiro, MEMBERS[pair.shiro], { save: false });
        // V2: Save lineup double-tapped.
        const save = lineupSide(page, pair.shiro).getByRole('button', { name: 'Save lineup' });
        const v2 = await doubleTap(save);
        // The first tap saves; the second finds the button busy or, once saved, off
        // (nothing is left to save). Saved: the side shows the lineup as this match's own.
        await expect(lineupSource(lineupSide(page, pair.shiro))).toHaveText('Lineup for this match');
        await rendered(page);
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
      // V2 on an ippon: two taps inside 300ms on Shiro's M. The app ignores the
      // second as a bounce, but the first mark makes the clear hint appear and the
      // row moves, so the second tap can land on a name box instead and open its list.
      const mBefore = await boutIpponButton(row, 'shiro', 'M').boundingBox();
      const v2 = await doubleTap(boutIpponButton(row, 'shiro', 'M'));
      await expect(boutFilled(currentBout(page), 'shiro')).toHaveCount(1);
      const mAfter = await boutIpponButton(currentBout(page), 'shiro', 'M').boundingBox();
      const marks = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
      const v2Shot = await shot(page, 'v2-ippon-double-tap');
      // Recover: close a fighter list the second tap opened, then tap the second
      // mark if there is one (the editor's "Tap a scored mark to clear it").
      const fighterListOpened = await closeNameList(page);
      const recoverTaps = await clearBoutMarks(currentBout(page), 'shiro', { keep: 1 });
      record({ step: 'M1 bout 1', action: 'Shiro M', variant: 'V2 doubleTap', ...v2, marksAfter: marks, fighterListOpened,
        mMovedByPx: mBefore && mAfter ? Math.round(mAfter.y - mBefore.y) : null,
        recoverTaps, hintShown: await editor(page).getByTestId('team-scoring-clear-hint').isVisible(), screenshot: v2Shot });
      // V1: the thumb lands on the button beside M.
      const v1 = await tapNeighbour(boutIpponButton(currentBout(page), 'shiro', 'M'), 'right');
      await rendered(page);
      const after = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
      record({ step: 'M1 bout 1', action: 'Shiro M (second point)', variant: 'V1 tapNeighbour right', ...v1, marksAfter: after,
        screenshot: await shot(page, 'v1-ippon-neighbour') });
      // Put the board where the operator meant it: M K for Shiro.
      await clearBoutMarks(currentBout(page), 'shiro', { keep: 1 });
      if ((await boutFilled(currentBout(page), 'shiro').count()) === 0) await awardBoutIppon(page, 'shiro', 'M');
      await awardBoutIppon(page, 'shiro', 'K');
      // V3: the iPad reloads the tab before Record bout was tapped. The wait
      // only lets writes land and is not asserted: whether the
      // marks survive the reload is the variant under audit (marksKept), and a
      // lost mark fails the asserts after Record bout below (expectPairing,
      // doneRowWinner).
      await settled(page).catch(() => {});
      const reload = await interruptC(page, 'reload');
      await expect(editor(page)).toBeVisible();
      const kept = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
      record({ step: 'M1 bout 1', action: 'scored bout, not yet recorded', variant: 'V3 interrupt reload', ...reload,
        marksKept: kept, doneRows: await doneRows(page).count(), pairing: await boutNames(currentBout(page)),
        screenshot: await shot(page, 'v3-reload-bout-1') });
      // V2 on Record bout: a re-tap while it reads "Saving…".
      const retap = await retapWhileSaving(recordBoutButton(page));
      await expect(doneRows(page)).not.toHaveCount(0);
      await expect(recordBoutButton(page)).not.toHaveText('Saving…');
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
      await rendered(page);
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
      // The thumb's first tap is a deliberate one: Record bout, a moment ago,
      // shares this place's bounce guard.
      await dwellOnSide(page, BOUT_CHANGE);
      const v2 = await doubleTap(remove, { afterRender: true });
      const removeTappedAt = Date.now();
      // The appended pairing is gone once the removal has been answered.
      await expect(remove).toBeHidden();
      const afterRemove = { doneRows: await doneRows(page).count(), current: await boutNames(currentBout(page)),
        currentShiroMarks: await boutFilled(currentBout(page), 'shiro').count(), currentAkaMarks: await boutFilled(currentBout(page), 'aka').count() };
      const removeShot = await shot(page, 'v2-remove-bout-double-tap');
      // Recover: Record bout again re-appends the same pairing. It shares
      // Remove this bout's bounce guard, so the operator's next tap there comes
      // after the dwell.
      await dwell(page, removeTappedAt);
      await recordBout(page);
      record({ step: 'M1 bout 3', action: '× Remove this bout', variant: 'V2 doubleTap', ...v2, removeBox, afterRemove,
        recoveredTo: await boutNames(currentBout(page)), recoverTaps: 1, screenshot: removeShot });
      await expectPairing(3, S[1], A[1]);

      const tie = tieButton(currentBout(page));
      // `from`: the label before the taps, so the settle cannot agree with the
      // page as it was before they were handled.
      const tieBefore = await tie.innerText();
      const v2tie = await doubleTap(tie);
      const tieLabel = (await untilStable(() => tie.innerText(), { from: tieBefore })).trim();
      record({ step: 'M1 bout 3', action: 'Tie (hikiwake)', variant: 'V2 doubleTap', ...v2tie, tieLabel,
        middle: await boutMiddle(currentBout(page)), screenshot: await shot(page, 'v2-tie-double-tap') });
      if (!/✓/.test(tieLabel)) await tie.tap();
      await expect(tie).toHaveText(/✓ Tie/);
      const v1tie = await tapNeighbour(tie, 'up');
      await rendered(page);
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
      const overtimeBefore = await enchoUndoButton(page).isVisible();
      const v2 = await doubleTap(enchoButton(page));
      const eyebrow = (await editor(page).locator('.editor-modal__eyebrow').first().innerText()).trim();
      // Undo encho appears beside Encho with the first tap and the row re-centres, so
      // the second tap can land on it and take the period straight back.
      const inOvertime = await untilStable(() => enchoUndoButton(page).isVisible(), { from: overtimeBefore });
      record({ step: 'M1 bout 4', action: 'Encho', variant: 'V2 doubleTap', ...v2, eyebrow, inOvertimeAfterDoubleTap: inOvertime,
        enchoStillOffered: await enchoButton(page).isVisible(), middle: await boutMiddle(currentBout(page)),
        screenshot: await shot(page, 'v2-encho-double-tap') });
      // The rest of the bout is fought in overtime: put the period back when the
      // double tap took it off.
      if (!inOvertime) await enchoButton(page).tap();
      await expect(enchoUndoButton(page)).toBeVisible();
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

    await test.step('bout 5: a hurried double tap lands one M, a second M tapped on purpose records M M for a win that was M K', async () => {
      const dbl = await doubleTap(boutIpponButton(currentBout(page), 'shiro', 'M'));
      // The app ignores the bounce of a tap: two taps inside its window land one M.
      await expect(boutFilled(currentBout(page), 'shiro')).toHaveCount(1);
      record({ step: 'M1 bout 5', action: 'Shiro M', variant: 'V2 doubleTap', ...dbl,
        marksAfter: await boutFilled(currentBout(page), 'shiro').allInnerTexts() });
      // The mistake the later correction is about: the operator, meaning M K,
      // taps M a second time, past the bounce window.
      if ((await boutFilled(currentBout(page), 'shiro').count()) < 2) {
        // Out of the bounce window counted from now, after the double tap's
        // clicks have all come: counted from before the double tap (its setup
        // in the page can take longer than the window on a loaded machine),
        // the deliberate tap could still fall inside it and be taken as the
        // bounce (seen once in a full run, stage 6 review 12).
        await dwell(page, Date.now());
        await boutIpponButton(currentBout(page), 'shiro', 'M').tap();
      }
      await expect(boutFilled(currentBout(page), 'shiro')).toHaveCount(2);
      await recordBout(page);
      await expectPairing(6, S[3], A[3]);
    });

    await test.step('bout 6: a point tapped offline, and the Back button mid-bout', async () => {
      let unsentWhileOffline;
      const offline = await interruptC(page, 'offline', {
        during: async (p) => {
          await boutIpponButton(currentBout(p), 'shiro', 'M').tap();
          await expect.poll(() => unsentCount(p)).toBeGreaterThan(0);
          unsentWhileOffline = await unsentCount(p);
        },
      });
      const resynced = await settled(page).then(() => true, () => false);
      record({ step: 'M1 bout 6', action: 'Shiro M', variant: 'V3 interrupt offline', ...offline, unsent: unsentWhileOffline, resynced,
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
      // Measured here: a fullPage screenshot leaves the page at a FINE pointer
      // afterwards, which would switch the coarse tap floors off. The camera
      // (fixtures/shots.mjs) puts the coarse pointer back in place, with no
      // reload; this row records the pointer before and after the shot, and
      // whether ensureCoarse still found it lost.
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
      // Opening or closing a correction flushes the live bout's save first, so
      // each tap below is read against the settled state before it (`from`): a
      // read straight after the tap can still be the pre-tap page.
      const openBeforeV1 = await untilStable(() => correctingBout(page).count());
      // V1: the thumb meant bout 5 and landed on the row below it.
      const v1 = await tapNeighbour(row5, 'down');
      await untilStable(() => correctingBout(page).count(), { from: openBeforeV1 });
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
      const openBeforeV2 = await untilStable(() => correctingBout(page).count());
      const v2 = await doubleTap(doneRows(page).nth(4));
      await untilStable(() => correctingBout(page).count(), { from: openBeforeV2 });
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
      await settled(page);
      // The sheet as the correction leaves it: whether the correction is open,
      // the bouts fought and bout 5's marks. A Back that reloads the court's
      // data changes one of them unless the correction survives it.
      const sheetNow = async () => ({
        open: await correctingBout(page).count(), done: await doneRows(page).count(), marks: await marksOf(4).catch(() => null),
      });
      const sheetBeforeBack = await sheetNow();
      const back = await interruptC(page, 'back');
      await expect(editor(page)).toBeVisible();
      // Whether the correction survives is what the row records: wait for the
      // sheet to leave its pre-Back state and stop changing (a correction that
      // survives changes nothing, which costs the helper's wait for a change).
      await untilStable(sheetNow, { from: sheetBeforeBack });
      record({ step: 'M1 correction', action: 'bout 5 open for correction', variant: 'V3 interrupt back', ...back,
        stillOpen: await correctingBout(page).count(), bout5: await boutNames(doneRows(page).nth(4)).catch(() => null),
        bout5Marks: await marksOf(4), live: await boutNames(currentBout(page)), screenshot: await shot(page, 'v3-back-mid-correction') });
      if (!(await correctingBout(page).count())) await doneRows(page).nth(4).tap();
      // V2 on the collapse triangle.
      const caretBtn = editor(page).getByTestId('kachinuki-done-collapse-4');
      const openBeforeV2c = await untilStable(() => correctingBout(page).count());
      const v2c = await doubleTap(caretBtn);
      const reopened = await untilStable(() => correctingBout(page).count(), { from: openBeforeV2c });
      const openedByV2c = reopened
        ? await editor(page).locator('[data-testid^="kachinuki-done-collapse-"]').first().getAttribute('data-testid') : null;
      record({ step: 'M1 correction', action: 'collapse triangle', variant: 'V2 doubleTap', ...v2c,
        openAfter: reopened, openedByV2c, screenshot: await shot(page, 'v2-collapse-double-tap') });
      // Collapse whatever the double tap left open: the second tap may have
      // landed on another bout's row once the first had collapsed bout 5.
      if (reopened) await editor(page).locator('[data-testid^="kachinuki-done-collapse-"]').first().tap();
      await expect(correctingBout(page)).toHaveCount(0);
      await settled(page);
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
      await clearBoutMarks(corr, 'shiro');
      // Two strikes for one side: the second waits out the first's bounce window.
      await tapOnSide(page, 'correction bout 1 aka', boutIpponButton(corr, 'aka', 'M'));
      await tapOnSide(page, 'correction bout 1 aka', boutIpponButton(corr, 'aka', 'M'));
      const warning = editor(page).getByTestId('kachinuki-done-edit-warn');
      const warned = await warning.isVisible();
      const warningText = warned ? (await warning.innerText()).trim() : '';
      const flipShot = await shot(page, 'bout-1-winner-flipped-warning');
      const laterUnchanged = await boutNames(currentBout(page));
      // Put it back as it was fought.
      await clearBoutMarks(corr, 'aka');
      await tapOnSide(page, 'correction bout 1 shiro', boutIpponButton(corr, 'shiro', 'M'));
      await tapOnSide(page, 'correction bout 1 shiro', boutIpponButton(corr, 'shiro', 'K'));
      await editor(page).getByTestId('kachinuki-done-collapse-0').tap();
      await settled(page);
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
      // V1: the thumb meant Fusensho and landed on the button above it (the
      // button has a line of its own, wide enough for its pressed label, F1).
      const v1 = await tapNeighbour(fus, 'up');
      await rendered(page);
      const marks = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
      const v1Shot = await shot(page, 'v1-fusensho-neighbour');
      const recoverTaps = await clearBoutMarks(currentBout(page), 'shiro');
      record({ step: 'M1 bout 8', action: 'Fusensho (Shiro)', variant: 'V1 tapNeighbour up', ...v1, shiroMarks: marks,
        recoverTaps, screenshot: v1Shot });
      // V2: Fusensho double-tapped (it is a toggle).
      const fusBefore = await fus.innerText();
      // F1 (operator decision 2026-10-19): the button holds its place when
      // pressed, so the second tap lands on it, never on H beside it.
      const fusBox = await fus.boundingBox();
      const v2 = await doubleTap(fus);
      const label = (await untilStable(() => fus.innerText(), { from: fusBefore })).trim();
      record({ step: 'M1 bout 8', action: 'Fusensho (Shiro)', variant: 'V2 doubleTap', ...v2, labelAfter: label,
        shiroMarks: await boutFilled(currentBout(page), 'shiro').allInnerTexts(), screenshot: await shot(page, 'v2-fusensho-double-tap') });
      if (!/✓/.test(label)) await fus.tap();
      await expect(fus).toHaveText(/✓ Fusensho/);
      // Where it was and as big. One pixel down is allowed: a tapped .btn
      // stays :active under touch emulation, and :active nudges it 1px
      // (translateY), the press feedback, not a layout move.
      const fusAfter = await fus.boundingBox();
      expect({ x: fusAfter.x, width: fusAfter.width, height: fusAfter.height }, 'pressed, the Fusensho button is where it was and as big')
        .toEqual({ x: fusBox.x, width: fusBox.width, height: fusBox.height });
      expect(Math.abs(fusAfter.y - fusBox.y), 'pressed, the Fusensho button stays on its line').toBeLessThanOrEqual(1);
      await shot(page, 'bout-8-fusensho');
      // V2 on End match: two taps inside 300ms on a two-tap guard.
      const end = endMatchButton(page);
      const endBox = await end.boundingBox();
      const v2end = await doubleTap(end);
      const completed = await completedRowFor(page, m1)
        .waitFor({ state: 'visible', timeout: 8000 }).then(() => true, () => false);
      const endLabelAfter = completed ? null : (await end.innerText().catch(() => null));
      record({ step: 'M1 end', action: 'End match (two-tap guard)', variant: 'V2 doubleTap', ...v2end, endBox,
        completedByDoubleTap: completed, endLabelAfter, screenshot: await shot(page, 'v2-end-match-double-tap') });
      if (!completed) {
        if (/^Tap again/.test(endLabelAfter || '')) await end.tap(); else await endMatch(page);
      }
      const row = completedRowFor(page, m1);
      await expect(row).toBeVisible();
      // Shiro won bouts 1, 5, 6 and 8 (the walkover's two maru), Aka 2 and 4:
      // IV 4-2, PW 8-3: bout 1 M K, bout 5 M K (corrected from M M), bout 6 M K
      // and the walkover's two maru for Shiro, bout 2 D M and bout 4 K for Aka
      // (bout 5's double tap scores one M since bc-dtip; before it the bounce added
      // a second). Bout 1 is put back to M K by the correction step above, each
      // strike paced past its side's bounce window; an unpaced pair left it at M
      // (the second strike swallowed) and this line read 7-3. Does the court's
      // own Completed record say so?
      const want = /IV 4–2\s+PW 8–3/;
      const result = row.locator('.shiaijo-qrow__result');
      const settledWithoutRefresh = await expect(result).toHaveText(want, { timeout: 6000 }).then(() => true, () => false);
      const shownAtFirst = (await result.innerText()).replace(/\s+/g, ' ');
      const staleShot = await shot(page, 'm1-completed');
      await page.getByRole('button', { name: 'Refresh' }).tap();
      await expect(completedRowFor(page, m1).locator('.shiaijo-qrow__result')).toHaveText(want);
      record({ step: 'M1 end', action: 'Completed row after End match', variant: 'result', expected: 'IV 4–2 PW 8–3',
        shownFor6s: shownAtFirst, settledWithoutRefresh,
        afterRefresh: (await completedRowFor(page, m1).locator('.shiaijo-qrow__result').innerText()).replace(/\s+/g, ' '),
        screenshot: staleShot });
      // Whatever the row showed before the refresh (recorded above), the totals
      // the bouts add up to must be what the Completed record settles on.
      await expect(completedRowFor(page, m1).locator('.shiaijo-qrow__result')).toHaveText(want, { timeout: 8000 });
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
      const v1 = await tapNeighbour(completedRowFor(page, m1).locator('.shiaijo-row__correct'), 'left')
        .catch((e) => ({ none: e.message.split('\n')[0] }));
      await rendered(page);
      const cb = await completedRowFor(page, m1).locator('.shiaijo-row__correct').boundingBox();
      record({ step: 'M1 reopen', action: 'Correct (Completed row)', variant: 'V1 tapNeighbour left', ...v1,
        correctBox: cb && `${Math.round(cb.width)}x${Math.round(cb.height)}`, screenshot: await shot(page, 'v1-correct-neighbour') });
      await completedRowFor(page, m1).locator('.shiaijo-row__correct').tap();
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
      await rendered(page);
      record({ step: 'M1 reopen', action: 'tap an ippon on the completed sheet', variant: 'V1 stray tap',
        buttonEnabled: enabled, akaMarksBefore: before, akaMarksAfter: await boutFilled(firstRow, 'aka').count(),
        saveButton: await editor(page).getByRole('button', { name: /Save correction/ }).count(),
        screenshot: await shot(page, 'v1-stray-tap-completed-sheet') });
    });

    await test.step('Reopen match, double-tapped', async () => {
      const reopen = reopenButton(page);
      const box = await reopen.boundingBox();
      const v2 = await doubleTap(reopen, { afterRender: true });
      await expect(recordBoutButton(page)).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      await rendered(page);
      record({ step: 'M1 reopen', action: 'Reopen match', variant: 'V2 doubleTap', ...v2, reopenBox: box,
        error: (await page.getByTestId('kachinuki-reopen-error').innerText().catch(() => '')).trim(),
        doneRows: await doneRows(page).count(), current: await boutNames(currentBout(page)),
        screenshot: await shot(page, 'v2-reopen-double-tap') });
    });

    await test.step('reload while reopened: back on the same encounter?', async () => {
      const reload = await interruptC(page, 'reload');
      const back = await editor(page).waitFor({ state: 'visible', timeout: EXPECT_TIMEOUT_MS }).then(() => true, () => false);
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
      await clearBoutMarks(currentBout(page), 'shiro');
      await awardBoutIppon(page, 'shiro', 'M');
      return wasFusensho;
    };
    const bout8Marks = () => boutFilled(currentBout(page), 'shiro').allInnerTexts();

    await test.step('fix bout 8: the walkover was really a men by Jin', async () => {
      // Bout 8 is the last bout; while reopened it is the live row.
      expect(await boutNames(currentBout(page))).toEqual({ shiro: MEMBERS[m1.shiro][4], aka: MEMBERS[m1.aka][4] });
      const wasFusensho = await fixBout8();
      await settled(page);
      record({ step: 'M1 reopen', action: 'undo fusensho, award M on bout 8', variant: 'correct path', wasFusensho,
        unsent: await unsentCount(page), screenshot: await shot(page, 'm1-bout-8-rescored') });
      // V3: the edit has landed (settled above), then the tab reloads.
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

    await test.step('End match on a reopened encounter asks for no reason: armed, then reloaded, then ended', async () => {
      // 9df3980a: ending a reopened match works like ending any match now, so
      // this is the same two-tap arm/confirm guard every End match uses
      // (endMatchButton), not a reason prompt.
      const end = endMatchButton(page);
      await end.tap();
      await expect(end).toHaveText(/^Tap again/);
      const armedLabel = (await end.innerText()).trim();
      await shot(page, 'm1-reopen-end-armed');
      // V3: the tab reloads while End match is armed but not yet confirmed.
      const reload = await interruptC(page, 'reload');
      await expect(editor(page)).toBeVisible();
      const stillRunning = await recordBoutButton(page).isVisible();
      const endedByReload = await completedRowFor(page, m1).isVisible();
      record({ step: 'M1 reopen', action: 'End match armed', variant: 'V3 interrupt reload', ...reload, armedLabel,
        stillRunning, endedByReload,
        bout8: await boutNames(currentBout(page)), bout8Marks: await boutFilled(currentBout(page), 'shiro').allInnerTexts(),
        screenshot: await shot(page, 'v3-reload-end-armed') });
      // The reload discards the client-only armed state without writing
      // anything: the encounter is still reopened and running, and bout 8
      // keeps the correction.
      expect(stillRunning).toBe(true);
      expect(endedByReload).toBe(false);
      if ((await bout8Marks()).join('') !== 'M') await fixBout8();
      // End for real, clumsily: two taps inside 300ms on the two-tap guard
      // (the same clumsy variant M1's first ending used above).
      const v2end = await doubleTap(endMatchButton(page));
      const completed = await completedRowFor(page, m1).waitFor({ state: 'visible', timeout: 8000 }).then(() => true, () => false);
      const endLabelAfter = completed ? null : (await endMatchButton(page).innerText().catch(() => null));
      record({ step: 'M1 reopen', action: 'End match (two-tap guard)', variant: 'V2 doubleTap', ...v2end,
        completedByDoubleTap: completed, endLabelAfter, screenshot: await shot(page, 'v2-end-match-double-tap-reopen') });
      if (!completed) {
        if (/^Tap again/.test(endLabelAfter || '')) await endMatchButton(page).tap(); else await endMatch(page);
      }
      const row = completedRowFor(page, m1);
      await expect(row).toBeVisible();
      await expect(row.locator('.shiaijo-qrow__result')).toContainText('IV');
      const immediately = (await row.locator('.shiaijo-qrow__result').innerText()).replace(/\s+/g, ' ');
      const immediateShot = await shot(page, 'm1-ended-again');
      // Bout 8 is now a men (1 point) instead of the walkover (2 maru).
      await expect(row.locator('.shiaijo-qrow__result')).toHaveText(/IV 4–2\s+PW 7–3/, { timeout: 8000 });
      record({ step: 'M1 reopen', action: 'ended again: Completed row', variant: 'result', expected: 'IV 4–2 PW 7–3',
        immediately, settledWithin8s: true, after: (await row.locator('.shiaijo-qrow__result').innerText()).replace(/\s+/g, ' '),
        screenshot: immediateShot });
      // Still Kawasemi's (Shiro's) win: the final keeps its side.
      await expect(page.locator('.shiaijo-pending, .shiaijo-upnext').first()).toContainText(m1.shiro);
    });
  });

  // The operator on A spots another mistake in M1, but the organiser has
  // already moved M2 onto A (shiaijo B's table is unstaffed) and it is running
  // there. The court console refuses Correct while a bout is live on its court
  // (bc-crpn), so the organiser reopens M1 from the Scores tab, which meets the
  // court-busy panel.
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
      await settled(page);
    });

    await test.step('Correct on M1 at the court console is refused while M2 runs', async () => {
      const refused = await correctRefusedAtConsole(page, m1, m2);
      record({ step: 'M1 reopen, court busy', action: 'Correct (Completed row) while M2 runs on A', variant: 'refused (bc-crpn)',
        ...refused, screenshot: await shot(page, 'm1-correct-refused-m2-running') });
    });

    const conflict = page.getByTestId('kachinuki-reopen-conflict');
    await test.step('Scores tab: Correct M1 -> Reopen match: the court is busy', async () => {
      await openScoresCorrection(page, compId, m1);
      await reopenButton(page).tap();
      await expect(conflict).toBeVisible();
      const req = await page.getByTestId('kachinuki-reopen-requeue-button').boundingBox();
      const leave = await page.getByTestId('kachinuki-reopen-conflict-dismiss').boundingBox();
      record({ step: 'M1 reopen, court busy', action: 'Reopen match (Scores tab) while M2 runs on A', variant: 'correct path',
        panel: (await conflict.innerText()).replace(/\s+/g, ' ').trim(),
        requeueBox: req && `${Math.round(req.width)}x${Math.round(req.height)}`,
        leaveBox: leave && `${Math.round(leave.width)}x${Math.round(leave.height)}`,
        screenshot: await shot(page, 'm1-reopen-court-busy') });
    });

    await test.step('V1: the thumb meant the danger button and hit Leave it running', async () => {
      const v1 = await tapNeighbour(page.getByTestId('kachinuki-reopen-requeue-button'), 'right');
      await rendered(page);
      const panelStill = await conflict.isVisible();
      record({ step: 'M1 reopen, court busy', action: 'Queue it and reopen (Scores tab)', variant: 'V1 tapNeighbour right',
        ...v1, panelStill, screenshot: await shot(page, 'v1-requeue-neighbour') });
      if (!panelStill) {
        await reopenButton(page).tap();
        await expect(conflict).toBeVisible();
      }
    });

    await test.step('V4: the panel answered by its loudest button without reading', async () => {
      const loud = await hastyPanel(conflict);
      await expect(conflict).toHaveCount(0, { timeout: EXPECT_TIMEOUT_MS });
      // The overlay follows the reopened match into bout mode.
      await expect(page.locator(EDITOR).getByRole('button', { name: 'Record bout' })).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      const overlayShot = await shot(page, 'v4-requeue-hasty');
      // M1 back in play is the court console's to show: it is A's live bout
      // again, with its bouts, and M2 is back in A's queue.
      await openShiaijo(page, 'A');
      await expect(recordBoutButton(page)).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      const upNext = await upNextCard(page).isVisible() ? await sides(upNextCard(page)) : null;
      record({ step: 'M1 reopen, court busy', action: 'court-busy panel (Scores tab)', variant: 'V4 hasty (loudest button)', ...loud,
        m1Reopened: await boutNames(currentBout(page)), doneRows: await doneRows(page).count(), upNext,
        screenshot: overlayShot, console: await shot(page, 'v4-requeue-hasty-console') });
      expect(await doneRows(page).count()).toBe(7);
      expect(upNext).toEqual(m2);
    });

    await test.step('M1 ended again (no reason owed, 9df3980a); M2 restarts with its score kept', async () => {
      // Ending a reopened match asks for no reason: the same two-tap
      // arm/confirm guard every End match uses.
      await endMatch(page);
      // M1 is complete again on the server. The console shows a result as soon as
      // End match is tapped, before the server has answered, so a page loaded
      // afresh is asked.
      await completedOnServer(page, m1);
      // The operator carries straight on: Start match on the Up next card.
      await expect(upNextCard(page)).toBeVisible();
      expect(await sides(upNextCard(page))).toEqual(m2);
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      await expect(recordBoutButton(page)).toBeVisible();
      // The reopen came from the Scores tab, so the console holds no
      // correction: M2 is the panel's live bout at once (V5's old lost place,
      // a started match hidden behind a pinned correction, is gone, bc-crpn).
      const afterStart = {
        upNextStill: await upNextCard(page).isVisible(),
        panelShows: (await editor(page).locator('.editor-modal__eyebrow').first().innerText().catch(() => '')).replace(/\s+/g, ' '),
        correctionBadge: await editor(page).getByText('Correction', { exact: true }).count(),
        m2Live: await recordBoutButton(page).isVisible(),
        backToCourt: await page.getByRole('button', { name: /Back to court/ }).count(),
      };
      record({ step: 'M1 reopen, court busy', action: 'End M1 again, then Start match on Up next (M2)', variant: 'V5 lost place',
        ...afterStart, screenshot: await shot(page, 'v5-m2-started-after-m1-ended') });
      await expect(recordBoutButton(page)).toBeVisible();
      // Nothing to recover from: the row stays to say so.
      record({ step: 'M1 reopen, court busy', action: 'recover the started M2', variant: 'V5 lost place', recoverTaps: 0,
        live: await boutNames(currentBout(page)) });
      expect(await boutNames(currentBout(page))).toEqual({ shiro: MEMBERS[m2.shiro][0], aka: MEMBERS[m2.aka][0] });
      const marks = await boutFilled(currentBout(page), 'aka').count();
      record({ step: 'M1 reopen, court busy', action: 'M2 restarted after the requeue', variant: 'result',
        m2Bout1AkaMarks: marks, panelSays: 'Sending it back to the queue keeps any score already entered for it: it carries on from there when it is started again',
        screenshot: await shot(page, 'm2-restarted-on-A') });
      // Sending a match back to the queue clears its verdict, never its points
      // (operator ruling 2026-09-26): Aka's first M is still on bout 1.
      expect(marks).toBe(1);
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
          const clearGhostMarks = () => clearBoutMarks(currentBout(page), 'shiro');
          // Correct path: one tap on the right fighter.
          await openList();
          await shot(page, 'm2-fighter-pick-open');
          const optBox = await option(S[1]).boundingBox();
          await option(S[1]).tap();
          await expect(option(S[1])).toBeHidden();
          // The pick is saved and the row redrawn: a tap made meanwhile is lost with the old row.
          await expect.poll(() => boutNames(currentBout(page))).toEqual({ shiro: S[1], aka: A[0] });
          await untilStable(() => boutNames(currentBout(page)));
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
          await expect(option(S[1])).toBeHidden();
          // The pick is saved and the row redrawn: a tap made meanwhile is lost with the old row.
          await untilStable(() => boutNames(currentBout(page)));
          const v2Marks = await shiroMarks();
          const v2Shot = await shot(page, 'm2-v2-fighter-pick-double-tap');
          record({ step: 'M2 bout 2', action: 'pick the Shiro fighter', variant: 'V2 doubleTap', ...v2, tapsToReopenList: reopenTaps,
            pairing: await boutNames(currentBout(page)), shiroMarks: v2Marks, recoverTaps: await clearGhostMarks(), screenshot: v2Shot });
          // A laptop operator closes the fighter list with Esc (the sheet's
          // own shortcut line says "Esc close").
          await openList();
          await page.keyboard.press('Escape');
          await rendered(page);
          const discard = page.locator('.modal[role="dialog"]').filter({ hasText: /Your unsaved entries will be discarded/ });
          const dialogShown = await discard.isVisible();
          const escShot = await shot(page, 'm2-esc-discard-dialog');
          let hasty = {};
          if (dialogShown) {
            hasty = await hastyConfirm(page);
            await expect(discard).toBeHidden();
          }
          record({ step: 'M2 bout 2', action: 'Esc to close the fighter list', variant: 'V4 hastyConfirm', dialogShown, ...hasty,
            editorStill: await recordBoutButton(page).isVisible(), pairing: await boutNames(currentBout(page)).catch(() => null),
            listStillOpen: await currentBout(page).locator('.team-sub-match__side--shiro .pmf__dropdown').isVisible(),
            unsent: await unsentCount(page),
            doneRows: await doneRows(page).count(), screenshot: escShot, after: await shot(page, 'm2-after-esc-discard') });
          // V1: the option below is hit instead.
          await openList();
          const v1 = await tapNeighbour(option(S[1]), 'down');
          await rendered(page);
          const wrong = await boutNames(currentBout(page));
          const v1Marks = await shiroMarks();
          const v1Shot = await shot(page, 'm2-v1-fighter-pick-neighbour');
          // Recover: clear any mark, pick the right fighter again.
          let recoverTaps = await clearGhostMarks();
          if (wrong.shiro !== S[1]) {
            recoverTaps += await openList();
            await option(S[1]).tap();
            recoverTaps += 1;
            await expect(option(S[1])).toBeHidden();
            // The pick is saved and the row redrawn: a tap made meanwhile is lost with the old row.
            await expect.poll(() => boutNames(currentBout(page))).toEqual({ shiro: S[1], aka: A[0] });
            await untilStable(() => boutNames(currentBout(page)));
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
          const v2 = await doubleTap(recordBoutButton(page), { afterRender: true });
          await expect(doneRows(page)).toHaveCount(before + 1);
          // Deliberate: the bounced second tap must NOT append another bout, and
          // an absence has no state to wait for, so the time is the evidence.
          await page.waitForTimeout(1200);
          record({ step: 'M2 bout 4', action: 'Record bout', variant: 'V2 doubleTap', ...v2,
            doneRows: await doneRows(page).count(), current: await boutNames(currentBout(page)),
            recordEnabled: await recordBoutButton(page).isEnabled(), screenshot: await shot(page, 'm2-v2-record-double-tap') });
        } else {
          // Bout 1 comes back with the M Aka scored before M2 was sent back to the
          // queue (the points stay), so it needs only the K.
          if ((await boutFilled(currentBout(page), 'aka').count()) === 0) {
            await winBout(page, 'aka');
          } else {
            await awardBoutIppon(page, 'aka', 'K');
            await recordBout(page);
          }
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
      // The second tap counts once the arming tap's bounce window has passed.
      await paceSettled(endMatchButton(page));
      await endMatchButton(page).tap();
      await expect(completedRowFor(page, m2)).toBeVisible();
      await shot(page, 'm2-completed');
    });

    await test.step('the hurried operator reopens M2 and taps Send back to queue to get out', async () => {
      await completedRowFor(page, m2).locator('.shiaijo-row__correct').tap();
      await reopenButton(page).tap();
      await expect(recordBoutButton(page)).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      const boutsBefore = await doneRows(page).count();
      const revert = page.getByRole('button', { name: 'Send back to queue' });
      await expect(revert).toBeVisible();
      await revert.tap();
      await shot(page, 'm2-send-back-confirm');
      const hasty = await hastyShiaijoConfirm(page);
      await expect(recordBoutButton(page)).toBeHidden();
      // Where did the encounter go, and did its bout log survive?
      const upNext = await upNextCard(page).isVisible() ? await sides(upNextCard(page)).catch(() => null) : null;
      let boutsAfter = null;
      if (upNext) {
        // Start is one match write; the sheet shows the bout log only once that
        // answer is in, so a count read before it is the empty sheet.
        const startAnswered = nextMatchWrite(page);
        await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
        await writeLanded(startAnswered, 'Start match');
        await expect(recordBoutButton(page)).toBeVisible();
        // Did the bout log survive the send back: the count once the sheet has loaded it.
        // The empty sheet (0) is the read before the log loads; `from` makes
        // the settle wait for it to leave that.
        boutsAfter = await untilStable(() => doneRows(page).count(), { from: 0 });
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
      if (await page.locator('.reason-prompt').isVisible()) {
        await hastyReasonPrompt(page);
      } else {
        // The second tap counts once the arming tap's bounce window has passed.
        await paceSettled(endMatchButton(page));
        await endMatchButton(page).tap();
      }
      await expect(completedRowFor(page, m2)).toBeVisible();
      await shot(page, 'm2-completed-again');
    });
  });

  test('J5 final: lineups carried over, the final starts on A', async ({ page }) => {
    await login(page);
    await openShiaijo(page, 'A');
    const final = await sides(upNextCard(page));
    // The final pairs the two semifinal winners: M1's Shiro and M2's Aka.
    expect([final.shiro, final.aka].sort()).toEqual([m1.shiro, m2.aka].sort());
    finalPair = final;
    await guarded(page, 'bc-crpn: Start match while a correction is open is refused', guardCrpn);

    await test.step('lineups for the final: each team carries its previous match\'s lineup', async () => {
      await openLineupFromUpNext(page);
      // There is nothing to copy: a team keeps the lineup of its previous team
      // match until one is saved for this match, and each side says whose it is
      // ("Same as <match>"). With nothing changed, neither side offers a Save.
      const sidesEls = page.locator('[data-testid^="match-lineup-side-"]');
      const carried = [];
      for (const [i, team] of [final.shiro, final.aka].entries()) {
        const side = sidesEls.nth(i);
        await expect(lineupSource(side)).toHaveText(/^Same as /);
        const inputs = side.locator('input.pmf__input');
        for (const [n, name] of MEMBERS[team].entries()) await expect(inputs.nth(n)).toHaveValue(name);
        await expect(side.getByRole('button', { name: 'Save lineup' })).toBeDisabled();
        carried.push({ team, source: (await lineupSource(side).innerText()).trim(),
          lineup: await inputs.evaluateAll((els) => els.map((e) => e.value)) });
      }
      record({ step: 'final lineup', action: 'open the lineup panel for the final', variant: 'carried over', carried,
        screenshot: await shot(page, 'final-lineups-carried') });
      await closeLineupPanel(page);
    });

    await test.step('start the final and score one point', async () => {
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      await expect(recordBoutButton(page)).toBeVisible();
      expect(await boutNames(currentBout(page))).toEqual({ shiro: MEMBERS[final.shiro][0], aka: MEMBERS[final.aka][0] });
      await awardBoutIppon(page, 'shiro', 'M');
      await settled(page);
      await shot(page, 'final-running-one-point');
    });

    await test.step('Correct M1 -> Reopen while the final (fed by M1) runs', async () => {
      // The court console refuses the Correct itself while the final is live
      // on A (bc-crpn).
      const refused = await correctRefusedAtConsole(page, m1, final);
      record({ step: 'final running', action: 'Correct M1 (Completed row) while the final runs on A', variant: 'refused (bc-crpn)',
        ...refused, screenshot: await shot(page, 'm1-correct-refused-final-running') });
      // The Scores tab still offers the reopen; the server refuses it because
      // the final M1 feeds is running.
      await openScoresCorrection(page, compId, m1);
      await reopenButton(page).tap();
      const err = page.getByTestId('kachinuki-reopen-error');
      await expect(err).toBeVisible();
      record({ step: 'final running', action: 'Reopen M1 (Scores tab) while its downstream final runs', variant: 'correct path',
        message: (await err.innerText()).trim(), courtBusyPanel: await page.getByTestId('kachinuki-reopen-conflict').count(),
        screenshot: await shot(page, 'm1-reopen-refused-downstream') });
      // Back to the live final on the court console.
      await openShiaijo(page, 'A');
      await expect(recordBoutButton(page)).toBeVisible();
      expect(await boutFilled(currentBout(page), 'shiro').count()).toBe(1);
    });
  });

  test('J5 final: played out, the knockout completes', async ({ page, browser, baseURL }) => {
    await login(page);
    await openShiaijo(page, 'A');
    await expect(recordBoutButton(page)).toBeVisible();
    await guarded(page, 'bc-emsl: tapping an empty mark slot leaves the bout\'s Tie on', guardEmsl);
    await guarded(page, 'bc-kheb: after one bout goes to encho, the next bout\'s header does not claim overtime', guardKheb);
    await guarded(page, 'bc-sync: a tapped point still unsent survives a reload', guardSync);
    const S = MEMBERS[finalPair.shiro];
    // Shiro keeps winning with whoever is on until End match; the operator
    // ends it when Aka is out of fighters. The Aka fighter changes each bout.
    // Play on from the first unfought bout: bc-kheb above records bout 1.
    for (let i = await doneRows(page).count(); i < 5; i += 1) {
      const live = currentBout(page);
      if (i === 3) {
        // V3: the tab reloads a moment after a point is tapped. Does the point survive?
        await boutIpponButton(live, 'shiro', 'M').tap();
        await page.waitForTimeout(150);
        const unsentAt150ms = await unsentCount(page);
        const reload = await interruptC(page, 'reload');
        await expect(recordBoutButton(page)).toBeVisible();
        const kept = await boutFilled(currentBout(page), 'shiro').allInnerTexts();
        record({ step: 'final bout 4', action: 'Shiro M, then the tab reloads 150ms later', variant: 'V3 interrupt reload', ...reload,
          unsentAt150ms, marksAfterReload: kept, screenshot: await shot(page, 'final-v3-reload-150ms-after-tap') });
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
        const appended = await expect(doneRows(page)).toHaveCount(before + 1, { timeout: EXPECT_TIMEOUT_MS }).then(() => true, () => false);
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
    await expect(completedRowFor(page, finalPair)).toBeVisible();
    // The console shows the result before the server has answered.
    await completedOnServer(page, finalPair, 'A');
    await shot(page, 'final-completed', { fullPage: true });

    // A spectator's phone: the finished knockout on the public viewer. The
    // final is in its Recent results, a result only a completed match gets,
    // with Shiro's team marked as the winner (viewer_match.jsx, the side's
    // `--w` class beside the "Winner:" text a screen reader hears).
    const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    try {
      const phone = await spectator.newPage();
      await openViewer(phone, compId);
      const finalResult = recentResult(phone, finalPair);
      await expect(finalResult).toContainText('Final');
      await expect(finalResult.locator('.vsched-item__side--w')).toHaveCount(1);
      await expect(finalResult.locator('.vsched-item__side--shiro.vsched-item__side--w')).toContainText(finalPair.shiro);
      await shot(phone, 'viewer-knockout-complete', { fullPage: true });
    } finally {
      await spectator.close();
    }
    // Last: the deferred guards (above) fail the test only now, after the
    // final and its viewer check ran.
    expect(guardFailures, 'regression guards that failed').toEqual([]);
  });
});

const m2State = {};
const pickState = {};
const correctionState = {};

// Waits until the server lists `pair` as completed on `court`, read by a page
// loaded afresh in the same context (so already signed in): the console on the
// page that tapped End match shows the result at once, before the server has
// answered, and keeps showing it if the server then holds the match unchanged.
async function completedOnServer(page, pair, court = 'A') {
  const fresh = await page.context().newPage();
  try {
    await expect.poll(async () => {
      await openShiaijo(fresh, court);
      return completedRowFor(fresh, pair).locator('.shiaijo-qrow__result').count();
    }, { timeout: EXPECT_TIMEOUT_MS, message: `${pair.shiro} v ${pair.aka} is completed on the server` }).toBeGreaterThan(0);
  } finally {
    await fresh.close();
  }
}

// Correct on `pair`'s Completed row while `running` is live on the court:
// refused (bc-crpn). The notice sits at the row, the same sentence as the
// toast; no correction opens and the live bout keeps the panel.
async function correctRefusedAtConsole(page, pair, running, court = 'A') {
  await completedRowFor(page, pair).locator('.shiaijo-row__correct').tap();
  const notice = completedRowFor(page, pair).locator('.shiaijo-upnext__error');
  await expect(notice).toHaveText(new RegExp(`^Shiaijo ${court} is running .*${running.shiro} vs ${running.aka}\\. `
    + 'Finish it or send it back to the queue first\\. Then correct this match\\.$'));
  const toast = (await page.locator('.toast').allInnerTexts().catch(() => [])).join(' | ');
  await expect(reopenButton(page)).toHaveCount(0);
  await expect(recordBoutButton(page)).toBeVisible();
  return { notice: (await notice.innerText()).trim(), toast, correctionOpened: await reopenButton(page).count() > 0,
    liveBout: await boutNames(currentBout(page)) };
}

// The organiser's door: Correct on `pair`'s row of the Scores tab opens the
// finished encounter in the overlay editor, which offers Reopen match.
async function openScoresCorrection(page, id, pair) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/scores`);
  const row = scoreRows(page).filter({ hasText: pair.shiro }).filter({ hasText: pair.aka }).first();
  await row.getByRole('button', { name: /^Correct$/ }).tap();
  await expect(page.locator(EDITOR)).toBeVisible();
  await expect(reopenButton(page)).toBeVisible();
}

// V4 for the inline reason prompt (ReasonPrompt): its loudest button, with
// whatever preset it offered by default.
async function hastyReasonPrompt(page) {
  const prompt = page.locator('.reason-prompt');
  await prompt.waitFor({ state: 'visible' });
  const preset = await prompt.locator('select').first().inputValue().catch(() => '');
  const { button, ...loud } = await loudestButton(prompt);
  await button.tap();
  await prompt.waitFor({ state: 'hidden', timeout: EXPECT_TIMEOUT_MS });
  return { ...loud, defaultReason: preset };
}

// V4 for an inline panel (the court-busy conflict): its loudest button.
async function hastyPanel(panel) {
  const { button, ...loud } = await loudestButton(panel);
  await button.tap();
  return loud;
}
