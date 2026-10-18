// Operator review journeys for INDIVIDUAL competitions (mp-yqxn.2, reviewer A).
//
// Every fixture is built through the interface (first-run form, wizard,
// roster paste box, Generate draw, Start), on this file's own server, in the
// journeys' beforeAll:
//   F1  pools + knockout, 12 entrants, shiaijo A and B      (J1)
//   F1b pools + knockout, 6 entrants, shiaijo E             (J2 decisions)
//   F2  knockout only, joint 3rd UNTICKED (bronze), shiaijo C (J2 hantei, J6)
//   F2d knockout only, default joint 3rd, shiaijo D         (J8 Bracket tab)
//   F1c pools + knockout, 6 entrants, shiaijo F             (J8 Pools tab)
// Each journey performs the correct path and the clumsy-operator variants
// (fixtures/clumsy.mjs, plus fixtures/individual.mjs), records one AUDIT row
// per action x variant (written to the journey folder's audit.json,
// fixtures/audit.mjs), and asserts only what the docs promise. Judgement
// (friction, legibility) is formed from the screenshots in the same folder
// (fixtures/shots.mjs), never asserted here. The journeys run in order and
// share their fixtures, so a failing journey skips the ones after it.
//
// The <bead-id> tests at the end are functional defects the journeys found,
// now fixed: each seeds its own competition on its own shiaijo and guards its
// fix. They sit in a describe of their own that is NOT serial, so a failing
// journey skips none of them: Playwright then runs them in a fresh worker,
// which gets a fresh server (fixtures/test.mjs serverForFile), and the
// file-level beforeAll below creates the tournament again there.
import { test, expect, isCoarse } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, EXPECT_TIMEOUT_MS } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import {
  openShiaijo, upNextCard, inlineEditor, sides, completedRows, completedRowFor, lastCompleted,
  editorIdentity, UP_NEXT,
} from '../fixtures/shiaijo.mjs';
import {
  INLINE_EDITOR, EDITOR, clearMarks, ipponButton, syncState, filledSlots, finishButton, armedFinishButton, armFinish, finishMatch,
  startMatch, awardIppon, nextTapWrite, writeLanded,
} from '../fixtures/scoring.mjs';
import { dwell, rendered, untilStable } from '../fixtures/pace.mjs';
import {
  doubleTap, hastyConfirm, hastyShiaijoConfirm, interrupt, retapWhileSaving, tapNeighbour,
} from '../fixtures/clumsy.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { holdScoreWrites } from '../fixtures/held-write.mjs';
import { tapSize } from '../fixtures/pointer.mjs';
import {
  ROSTER_12, ROSTER_6, ROSTER_4_BRONZE, ROSTER_4_JOINT, ROSTER_6B, rosterOf,
  editorSides, slotMarks, winnerOf, inlineRunning, ensureRunning, playRunningBout,
  decisionButton, decisionPrompt, recordDecision, queuedPoolBouts, correctCompleted,
} from '../fixtures/individual.mjs';

const comps = {};
const audit = auditLog('knockout-mixed-individual');

// The tournament, once per worker. A worker runs this file on one server; a
// worker started after a failure gets a fresh, empty one, so this runs again
// there (see the header).
test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
  const server = await serverForFile(testInfo.file);
  const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: server.base });
  await createTournament(await ctx.newPage(), { name: 'Individual Review Cup', courts: 15 });
  await ctx.close();
});

// File level, so the bead guards' rows (the console policy's warnings) are
// written as well.
test.afterAll(() => audit.flush());

// Let the running bout's taps land. The pill reads "Syncing…" from the tap
// until it has landed (each tap is an outbox entry in the tick it is made, and
// the pill has re-rendered by the time tap() returns), so "Synced" after a tap
// means the write went out and came back.
async function settle(page, root = INLINE_EDITOR) {
  await expect(syncState(page, root)).toHaveText('Synced');
}

// Which side's cell of a score string carries the Ht mark: 'shiro', 'aka' or
// null. A score string reads [Shiro cell] [middle] [Aka cell], the middle one
// of vs, X, (E), (DH), and Ht rides beside the winner, never in the middle
// ("M Ht (E) K" is Shiro's hantei, "M (E) K Ht" Aka's).
function htSide(score) {
  const m = /^(.*?)(?:^|\s)(vs|X|\(E\)|\(DH\))(?:\s|$)(.*)$/.exec(score.trim());
  if (!m) return null;
  if (/\bHt\b/.test(m[1])) return 'shiro';
  if (/\bHt\b/.test(m[3])) return 'aka';
  return null;
}

test.describe('knockout-mixed-individual', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    test.setTimeout(240_000);
    const server = await serverForFile(testInfo.file);
    const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: server.base });
    const page = await ctx.newPage();
    await login(page); // the file-level beforeAll created the tournament, in its own context
    const seed = async (key, opts, roster) => {
      comps[key] = await createCompetition(page, opts);
      await pasteRoster(page, comps[key], roster);
      await generateDraw(page, comps[key]);
      await startCompetition(page, comps[key]);
    };
    await seed('F1', { name: 'Men Individual', kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A', 'B'], numberPrefix: 'M' }, ROSTER_12);
    await seed('F1b', { name: 'Women Individual', kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['E'], numberPrefix: 'W' }, ROSTER_6);
    await seed('F2', { name: 'Veterans Knockout', kind: 'individual', format: 'knockout', twoThirdPlaces: false, courts: ['C'], numberPrefix: 'V' }, ROSTER_4_BRONZE);
    await seed('F2d', { name: 'Juniors Knockout', kind: 'individual', format: 'knockout', courts: ['D'], numberPrefix: 'J' }, ROSTER_4_JOINT);
    await seed('F1c', { name: 'Cadets Individual', kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['F'], numberPrefix: 'C' }, ROSTER_6B);
    await ctx.close();
  });

  // J1. Individual pools -> knockout on /admin/shiaijo/A (F1). Every bout is
  // chained with Finish + Start Next, which must never leave shiaijo A, and
  // the clumsy variants are performed on each kind of action on the way.
  test('J1 shiaijo console: pools to knockout without leaving the court', async ({ page }) => {
    test.setTimeout(300_000);
    const J = 'J1';
    const shot = journeyShots('knockout-mixed-individual/J1');
    const row = (r) => audit.record({ journey: J, ...r });
    await login(page);
    await openShiaijo(page, 'A');
    expect(await isCoarse(page)).toBe(true);
    await shot(page, 'court-A-idle');

    // Start match from the Up next card, twice inside 300ms.
    const card = upNextCard(page);
    const firstPair = await sides(card);
    const startBtn = card.getByRole('button', { name: /Start match|Starting…/ });
    row({ step: 'start bout', action: 'Start match (Up next card)', variant: 'size', ...(await tapSize(startBtn)) });
    const dbl = await doubleTap(startBtn);
    await expect.poll(() => inlineRunning(page), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    const running1 = await editorSides(page);
    const inProgress = (await page.locator('.admin-live-strip, .live-strip').allInnerTexts()).join(' ');
    await expect.poll(async () => JSON.stringify(await sides(upNextCard(page)).catch(() => null))).not.toBe(JSON.stringify(running1));
    const upNextAfter = await sides(upNextCard(page)).catch(() => null);
    row({ step: 'start bout', action: 'Start match (Up next card)', variant: 'V2 doubleTap', ...dbl,
      intended: firstPair, editorHolds: running1, upNextAfter, liveStrip: inProgress,
      shot: await shot(page, 'start-double-tapped') });
    expect(running1).toEqual(firstPair);

    const id1 = await editorIdentity(page);
    const w1 = winnerOf(running1);
    const other = w1 === 'shiro' ? 'aka' : 'shiro';

    // Award an ippon: first the neighbour of the intended M (V1), cleared by a
    // tap on the mark; then a double tap on M (V2).
    const ippon = ipponButton(page, w1, 'M');
    row({ step: 'award ippon', action: 'ippon button', variant: 'size', ...(await tapSize(ippon)) });
    const nb = await tapNeighbour(ippon, 'right');
    await expect(syncState(page)).toHaveText('Synced');
    const afterNb = { [w1]: await slotMarks(page, w1), [other]: await slotMarks(page, other) };
    row({ step: 'award ippon', action: `${w1} M`, variant: 'V1 tapNeighbour right', hit: nb.hit.label, marks: afterNb,
      shot: await shot(page, 'ippon-neighbour-tapped') });
    // Recover: tap the wrong mark to clear it.
    const wrongSlot = inlineEditor(page).locator(`[aria-label^="${w1 === 'shiro' ? 'Shiro' : 'Aka'} slot "][aria-label*=": remove "]`).first();
    const slotSize = await tapSize(wrongSlot);
    const hintVisible = await inlineEditor(page).getByTestId('scoring-modal-clear-hint').isVisible().catch(() => false);
    await wrongSlot.tap();
    await expect(filledSlots(page, w1)).toHaveCount(0);
    row({ step: 'award ippon', action: 'clear a wrong mark (tap the mark)', variant: 'recovery', taps: 1, slotSize, hintVisible });
    await settle(page); // the removal lands before the next strike (see the reset below)

    const dblIppon = await doubleTap(ipponButton(page, w1, 'M'));
    await expect(syncState(page)).toHaveText('Synced');
    const afterDbl = await slotMarks(page, w1);
    row({ step: 'award ippon', action: `${w1} M`, variant: 'V2 doubleTap', ...dblIppon, marks: afterDbl,
      shot: await shot(page, 'ippon-double-tapped') });
    // The side was empty (the wrong mark was cleared above): a bounce would land
    // two marks, so exactly one is the app ignoring it.
    expect(afterDbl).toEqual(['M']);

    // V2 on the undo: a double tap on the outer mark.
    const outer = inlineEditor(page).locator(`[aria-label^="${w1 === 'shiro' ? 'Shiro' : 'Aka'} slot "][aria-label*=": remove "]`).first();
    const dblClear = await doubleTap(outer);
    await expect(syncState(page)).toHaveText('Synced');
    const afterDblClear = await slotMarks(page, w1);
    row({ step: 'clear mark', action: 'tap a scored mark', variant: 'V2 doubleTap', ...dblClear,
      marksBefore: afterDbl, marksAfter: afterDblClear, shot: await shot(page, 'mark-double-tapped') });
    // Put the bout back to one men for the winner.
    await clearMarks(filledSlots(page, w1), { keep: 1, from: 'first' });
    if ((await filledSlots(page, w1).count()) === 0) {
      await settle(page); // the same reset race as below: let a removal land first
      await ipponButton(page, w1, 'M').tap();
    }
    await expect(filledSlots(page, w1)).toHaveCount(1);
    await settle(page);

    // V3 edge: a reload INSIDE the 300ms autosave debounce, right after a tap.
    // The pill reads "Syncing…" from the tap (pillAtTap records what it shows),
    // and the page-hide flush writes the pending edit before the page goes.
    await ipponButton(page, other, 'K').tap();
    const pillAtTap = (await syncState(page).innerText()).trim();
    await interrupt(page, 'reload');
    await expect.poll(() => inlineRunning(page), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    await expect(syncState(page)).toHaveText('Synced');
    const otherAfterFastReload = await slotMarks(page, other);
    row({ step: 'running bout', action: `${other} K then reload within 300ms`, variant: 'V3 interrupt reload (inside autosave debounce)',
      pillAtTap, marksAfterReload: otherAfterFastReload, shot: await shot(page, 'fast-reload-after-tap') });
    // Whatever survived, go back to one men for the winner and nothing else.
    for (const sd of [other, w1]) await clearMarks(filledSlots(page, sd), { from: 'first', pauseMs: 150 });
    // Let the removals land before the M. A fresh-clone run lost this tap to
    // the removal's echo (bc-ltap, fixed in the same PR and guarded by its own
    // test below); this step only resets the bout, so it does not race.
    await settle(page);
    await ipponButton(page, w1, 'M').tap();
    await expect(filledSlots(page, w1)).toHaveCount(1);
    await expect(filledSlots(page, other)).toHaveCount(0);
    await settle(page);

    // V3 interruptions mid-bout, and V5: same court, same match, marks kept?
    for (const kind of ['reload', 'hidden', 'back', 'offline']) {
      const r = await interrupt(page, kind);
      const back = await expect.poll(() => inlineRunning(page), { timeout: EXPECT_TIMEOUT_MS }).toBe(true).then(() => true, () => false);
      const id = back ? await editorIdentity(page) : null;
      const marks = back ? await slotMarks(page, w1) : null;
      row({ step: 'running bout, one ippon', action: 'interruption', variant: `V3 interrupt ${kind}`, ...r,
        editorBack: back, sameMatch: !!id && id.eyebrow === id1.eyebrow && id.court === 'A', marksKept: marks,
        shot: await shot(page, `interrupt-${kind}`) });
      expect(id && id.court).toBe('A');
    }

    // Finish, double-tapped: does the two-tap guard hold?
    const finish = finishButton(page);
    row({ step: 'finish', action: 'Finish + Start Next', variant: 'size', label: (await finish.innerText()).trim(), ...(await tapSize(finish)) });
    const dblFinish = await doubleTap(finish);
    await expect(completedRowFor(page, running1).or(armedFinishButton(page))).toBeVisible();
    // A tap inside the arm's bounce window is ignored: out it, so the tap below counts.
    await dwell(page, Date.now());
    const finishedByDouble = await completedRowFor(page, running1).isVisible().catch(() => false);
    const armedLeft = await armedFinishButton(page).isVisible().catch(() => false);
    row({ step: 'finish', action: 'Finish + Start Next', variant: 'V2 doubleTap', ...dblFinish,
      finishedByDoubleTap: finishedByDouble, stillArmed: armedLeft, shot: await shot(page, 'finish-double-tapped') });
    if (!finishedByDouble) {
      if (!armedLeft) await armFinish(page);
      await armedFinishButton(page).tap();
    }
    const r1 = await lastCompleted(page);
    expect(r1).toMatchObject({ shiro: running1.shiro, aka: running1.aka });
    expect(r1.result).toMatch(/M/);

    // Finish + Start Next started the next bout on THIS court.
    await expect.poll(() => inlineRunning(page), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    const id2 = await editorIdentity(page);
    const running2 = await editorSides(page);
    expect(id2.court).toBe('A');
    expect(running2).not.toEqual(running1);
    await shot(page, 'bout-2-started-by-finish-next');

    // Bout 2: V1 on the armed Finish, then a re-tap while it is saving.
    await ipponButton(page, winnerOf(running2), 'M').tap();
    await expect(syncState(page)).toHaveText('Synced');
    const armed = await armFinish(page);
    let nbFinish;
    try {
      nbFinish = await tapNeighbour(armed, 'left');
    } catch (e) {
      nbFinish = { hit: null, note: String(e.message).slice(0, 120) };
    }
    await rendered(page);
    row({ step: 'finish', action: 'armed Finish', variant: 'V1 tapNeighbour left', hit: nbFinish.hit ? nbFinish.hit.label : null,
      note: nbFinish.note, stillArmed: await armedFinishButton(page).isVisible().catch(() => false),
      editorCourt: (await editorIdentity(page)).court, shot: await shot(page, 'finish-neighbour-tapped') });
    if (!(await armedFinishButton(page).isVisible().catch(() => false))) await armFinish(page);
    const retap = await retapWhileSaving(armedFinishButton(page));
    await expect(completedRowFor(page, running2)).toBeVisible();
    await expect.poll(() => inlineRunning(page), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    const running3 = await editorSides(page);
    const completedCount = await completedRows(page).count();
    row({ step: 'finish', action: 'armed Finish', variant: 'V2 retapWhileSaving', ...retap,
      completedRows: completedCount, nextRunning: running3, court: (await editorIdentity(page)).court,
      shot: await shot(page, 'finish-retapped-while-saving') });
    expect((await editorIdentity(page)).court).toBe('A');

    // Bout 3: an ippon, then "Send back to queue" answered hastily.
    await ipponButton(page, winnerOf(running3), 'M').tap();
    await settle(page);
    const sendBack = page.getByRole('button', { name: 'Send back to queue' });
    row({ step: 'send back', action: 'Send back to queue', variant: 'size', ...(await tapSize(sendBack)) });
    const marksBeforeSendBack = await slotMarks(page, winnerOf(running3));
    await sendBack.tap();
    await page.locator('.shiaijo-move-confirm[role="dialog"]').waitFor({ state: 'visible' });
    const sendBackDialogShot = await shot(page, 'send-back-dialog');
    const hasty = await hastyShiaijoConfirm(page);
    await expect.poll(() => inlineRunning(page)).toBe(false);
    const idleAfter = await page.locator('.shiaijo__placeholder').isVisible().catch(() => false);
    const upNextAfterRevert = await sides(upNextCard(page)).catch(() => null);
    row({ step: 'send back', action: 'Send back to queue confirm', variant: 'V4 hastyConfirm', ...hasty,
      marksOnBoard: marksBeforeSendBack, dialogShot: sendBackDialogShot, courtIdle: idleAfter, upNext: upNextAfterRevert,
      shot: await shot(page, 'send-back-hasty') });
    // It is back in the queue; start it again and play it.
    const again = await ensureRunning(page);
    expect(again, 'the sent-back bout is Up next and starts again').not.toBeNull();
    row({ step: 'send back', action: 'restart the sent-back bout', variant: 'recovery',
      restarted: again, sameAsSentBack: !!again && again.shiro === running3.shiro && again.aka === running3.aka,
      marksAfterRestart: again ? await slotMarks(page, winnerOf(again)) : null });

    // Play out court A's pool bouts but the last one.
    for (let i = 0; i < 8 && (await queuedPoolBouts(page)) > 0; i += 1) await playRunningBout(page);
    await expect.poll(() => inlineRunning(page), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    expect((await editorIdentity(page)).eyebrow).toMatch(/POOL/);
    await shot(page, 'court-A-last-pool-bout-running');

    // Correct a completed bout while the last pool bout runs. The console
    // refuses it (bc-crpn): the notice at the row says to finish the live bout
    // or send it back first. The operator sends it back (its score is kept),
    // corrects, and starts it again.
    const firstDone = completedRowFor(page, running1);
    const correctBtn = firstDone.getByRole('button', { name: 'Correct' });
    row({ step: 'correct', action: 'Correct (completed row)', variant: 'size', ...(await tapSize(correctBtn)) });
    const lastPoolLive = await editorSides(page);
    const lastPoolMarks = await slotMarks(page, winnerOf(lastPoolLive));
    const { live: refusedFor, refusal } = await correctCompleted(page, running1);
    row({ step: 'correct', action: 'Correct while the last pool bout runs', variant: 'correct path (refused, bc-crpn)',
      liveBout: refusedFor, ...refusal, shot: await shot(page, 'correct-refused-then-sent-back') });
    expect(refusedFor).toEqual(lastPoolLive);
    const corrId = await editorIdentity(page);
    await shot(page, 'correcting-bout-1');
    await ipponButton(page, w1, 'K').tap();
    await inlineEditor(page).getByRole('button', { name: 'Save correction' }).tap();
    const reason = inlineEditor(page).locator('.reason-prompt');
    await expect(reason).toBeVisible();
    await shot(page, 'correction-reason');
    await reason.locator('button.btn--primary').tap();
    await expect(reason).toBeHidden();
    const backToCourt = page.getByRole('button', { name: /Back to court/ });
    const stillCorrecting = await backToCourt.isVisible().catch(() => false);
    row({ step: 'correct', action: 'Save correction (reason prompt, loud button)', variant: 'V4 hasty (inline prompt)',
      editorCourt: corrId.court, stayedOnCorrection: stillCorrecting, result: (await completedRowFor(page, running1).locator('.shiaijo-qrow__result').innerText()).trim(),
      shot: await shot(page, 'correction-saved') });
    if (stillCorrecting) await backToCourt.tap();
    // Start the sent-back bout again: it is Up next, with the point it had.
    const lastPoolA = await ensureRunning(page);
    expect(lastPoolA, 'the bout sent back for the correction starts again').not.toBeNull();
    const marksAfterRestart = await slotMarks(page, winnerOf(lastPoolA));
    row({ step: 'correct', action: 'restart the bout sent back for the correction', variant: 'recovery',
      restarted: lastPoolA, marksBefore: lastPoolMarks, marksAfterRestart, shot: await shot(page, 'back-to-live-bout') });
    expect(lastPoolA).toEqual(lastPoolLive);
    expect(marksAfterRestart).toEqual(lastPoolMarks);

    // Court B plays out all its pool bouts, so court A's last bout is the one
    // that closes the pool phase.
    await openShiaijo(page, 'B');
    const bOrder = [];
    for (let i = 0; i < 12; i += 1) {
      const running = await inlineRunning(page);
      const eyebrow = running ? (await editorIdentity(page)).eyebrow : '';
      if ((await queuedPoolBouts(page)) === 0 && !/POOL/.test(eyebrow)) break;
      const played = await playRunningBout(page);
      bOrder.push({ ...played, eyebrow: eyebrow || '(started from Up next)' });
      // Did Finish + Start Next leave the court idle with pool bouts queued?
      // Give it the time a start takes; an idle court is what the row records.
      const restarted = await expect.poll(() => inlineRunning(page), { timeout: 1500 }).toBe(true).then(() => true, () => false);
      if (!restarted && (await queuedPoolBouts(page)) > 0) {
        const toasts = (await page.locator('.toast, [role="status"]').allInnerTexts()).join(' | ');
        row({ step: 'court B pools', action: `Finish + Start Next after ${eyebrow || 'a bout'}`, variant: 'correct path',
          happened: 'court left idle although pool bouts wait in Up next', upNext: await sides(upNextCard(page)).catch(() => null),
          label: 'Finish + Start Next', toasts, shot: await shot(page, 'court-B-idle-after-finish-next') });
      }
    }
    row({ step: 'court B pools', action: 'Finish + Start Next through court B', variant: 'correct path',
      order: bOrder.map((b) => b.eyebrow) });
    await shot(page, 'court-B-pools-done');

    // Back on A: the last pool bout, finished with Finish + Start Next.
    await openShiaijo(page, 'A');
    await expect.poll(() => inlineRunning(page), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    expect(await editorSides(page)).toEqual(lastPoolA);
    const labelAtBoundary = (await finishButton(page).innerText()).trim();
    await ipponButton(page, winnerOf(lastPoolA), 'M').tap();
    await expect(syncState(page)).toHaveText('Synced');
    await finishMatch(page, INLINE_EDITOR);
    await expect(completedRowFor(page, lastPoolA)).toBeVisible();
    // Deliberate: a settle before an audit row only; nothing is asserted on it.
    await page.waitForTimeout(2500);
    const afterBoundary = {
      editorRunning: await inlineRunning(page),
      idle: await page.locator('.shiaijo__placeholder').isVisible().catch(() => false),
      upNext: await sides(upNextCard(page)).catch(() => null),
      upNextHeading: ((await page.locator('.shiaijo-upnext__time').allInnerTexts())[0] || '').trim(),
      court: (await page.getByRole('heading', { level: 1 }).first().innerText()).trim(),
    };
    row({ step: 'pool -> knockout boundary', action: 'Finish + Start Next on the last pool bout', variant: 'correct path',
      labelAtBoundary, ...afterBoundary, shot: await shot(page, 'boundary-after-last-pool-bout') });
    expect(afterBoundary.court).toMatch(/Shiaijo A/);

    // Two knockout bouts on A, chained.
    for (let i = 0; i < 2; i += 1) {
      const p = await ensureRunning(page);
      expect(p).not.toBeNull();
      const idk = await editorIdentity(page);
      expect(idk.court).toBe('A');
      await playRunningBout(page);
      row({ step: `knockout bout ${i + 1}`, action: 'Finish + Start Next', variant: 'correct path', pair: p, eyebrow: idk.eyebrow,
        court: idk.court });
    }
    await shot(page, 'court-A-knockout-chained');

    // Docs (Score a match): on a laptop, Left / Right move to the previous or
    // next match on the court and Esc closes the editor. Tried on the console.
    // A third knockout bout is running or Up next on A: the draw and the
    // winner rule are fixed, so the court always has one here.
    const liveK = await ensureRunning(page);
    expect(liveK, 'the next knockout bout on court A starts').not.toBeNull();
    const before = await editorIdentity(page);
    await inlineEditor(page).locator('.editor-modal__title').click();
    const seenK = [];
    for (const key of ['ArrowRight', 'ArrowLeft', 'Escape']) {
      await page.keyboard.press(key);
      await rendered(page);
      seenK.push({ key, eyebrow: (await editorIdentity(page).catch(() => ({ eyebrow: null }))).eyebrow,
        editor: await inlineEditor(page).isVisible().catch(() => false) });
    }
    row({ step: 'console keys', action: 'ArrowRight / ArrowLeft / Esc on the court console', variant: 'correct path (docs: laptop keys)',
      before: before.eyebrow, after: seenK, shot: await shot(page, 'console-keys') });

    // Scores tab overlay: Prev / Next and the arrow keys stay on the court.
    await page.goto(`/admin/competition/${comps.F1}/scores`);
    const rowB = page.locator('.score-edit-row').filter({ has: page.locator('.score-edit-row__court', { hasText: /^B\b/ }) })
      .filter({ has: page.getByRole('button', { name: /^Score$/ }) }).first();
    await rowB.getByRole('button', { name: /^Score$/ }).tap();
    await expect(page.locator(EDITOR)).toBeVisible();
    const seen = [await editorIdentity(page, EDITOR)];
    await shot(page, 'scores-overlay-B');
    const next = page.locator(EDITOR).locator('.score-nav__next');
    const prev = page.locator(EDITOR).locator('.score-nav__prev');
    row({ step: 'scores tab nav', action: 'Prev / Next', variant: 'size', next: await tapSize(next).catch(() => null), prev: await tapSize(prev).catch(() => null) });
    // Court B holds more matches than the first, so Next is offered (it is
    // rendered only while there is a next match). Each tap must move the
    // editor to another match before the reading is taken.
    await expect(next).toBeVisible();
    for (let i = 0; i < 3 && await next.isVisible().catch(() => false); i += 1) {
      const was = seen[seen.length - 1].key;
      await next.tap();
      await expect.poll(async () => (await editorIdentity(page, EDITOR)).key, { message: 'Next moves the editor' }).not.toBe(was);
      seen.push(await editorIdentity(page, EDITOR));
    }
    expect(new Set(seen.map((s) => s.key)).size, 'Next showed at least two matches').toBeGreaterThanOrEqual(2);
    // Docs (scoring-a-match.md): Left and Right move to the previous or next
    // match on the court, so each key must land on the neighbour the key names.
    const atEnd = seen[seen.length - 1].key;
    const keyed = [];
    for (const key of ['ArrowLeft', 'ArrowRight', 'ArrowLeft']) {
      const was = (await editorIdentity(page, EDITOR)).key;
      await page.keyboard.press(key);
      await expect.poll(async () => (await editorIdentity(page, EDITOR)).key, { message: `${key} moves the editor` }).not.toBe(was);
      const now = await editorIdentity(page, EDITOR);
      keyed.push({ key, from: was, to: now.key });
      seen.push(now);
    }
    expect(keyed[0].to, 'Left goes to the previous match').not.toBe(atEnd);
    expect(keyed[1].to, 'Right comes back').toBe(atEnd);
    expect(keyed[2].to, 'Left again goes to the same previous match').toBe(keyed[0].to);
    row({ step: 'scores tab nav', action: 'Next x3, ArrowLeft/Right', variant: 'correct path', courts: seen.map((s) => s.court), eyebrows: seen.map((s) => s.eyebrow),
      shot: await shot(page, 'scores-overlay-after-nav') });
    for (const s of seen) expect(s.court).toBe('B');
  });

  // J2. Special decisions. On F1b (court E, pools): a kiken recorded against
  // the WRONG side by a hasty operator (the prompt picks no side, bc-dsid, so
  // they take the first one offered), the fusensho recorded from the withdrawn
  // competitor's next match, then the undo, which asks no confirm (bc-dlck);
  // a fusenpai that auto-advances. On F2 (court C, knockout): a tied 1-1 bout
  // through encho (an unbounded counter) to hantei, picked then Finished
  // (bc-htsd).
  test('J2 special decisions: kiken chain and undo, fusenpai, encho, hantei', async ({ page }) => {
    test.setTimeout(240_000);
    const J = 'J2';
    const shot = journeyShots('knockout-mixed-individual/J2');
    const row = (r) => audit.record({ journey: J, ...r });
    await login(page);
    await openShiaijo(page, 'E');

    // Pool A's first bout. The competitor who withdraws is AKA.
    const b1 = await ensureRunning(page);
    expect(b1, 'a pool bout starts on this court').not.toBeNull();
    const id1 = await editorIdentity(page);
    expect(id1.eyebrow).toMatch(/POOL A/);
    for (const kind of ['kiken-voluntary', 'kiken-injury', 'fusenpai']) {
      row({ step: 'decision controls', action: kind, variant: 'size', ...(await tapSize(decisionButton(page, kind))) });
    }
    row({ step: 'decision controls', action: 'Decide by hantei…', variant: 'size',
      ...(await tapSize(inlineEditor(page).getByTestId('scoring-modal-hantei-arm'))) });

    // V1: the thumb meant Kiken - Voluntary and landed on its neighbour.
    const nbKiken = await tapNeighbour(decisionButton(page, 'kiken-voluntary'), 'right');
    await rendered(page);
    const promptOpen = await decisionPrompt(page).isVisible().catch(() => false);
    const popover = await page.locator('[role="tooltip"], .glossary-pop, .term-pop').first().isVisible().catch(() => false);
    row({ step: 'kiken', action: 'Kiken - Voluntary', variant: 'V1 tapNeighbour right', hit: nbKiken.hit.label, gapPx: nbKiken.gapPx,
      promptOpened: promptOpen, glossaryOpened: popover, committed: false, shot: await shot(page, 'kiken-neighbour-tapped') });
    if (promptOpen) await decisionPrompt(page).getByRole('button', { name: 'Cancel' }).tap();
    await page.keyboard.press('Escape').catch(() => {});

    // V4: the prompt is answered by its loud button without choosing a side.
    // No side is picked for the operator and Record stays off until one is
    // (bc-dsid), so the hasty tap records nothing.
    await decisionButton(page, 'kiken-voluntary').tap();
    await expect(decisionPrompt(page)).toBeVisible();
    await shot(page, 'kiken-prompt');
    const preselected = await decisionPrompt(page).locator('input[name="decision-side"]:checked').count();
    const radio = decisionPrompt(page).locator('input[name="decision-side"]').first();
    row({ step: 'kiken', action: 'side pill (Which side withdrew?)', variant: 'size', ...(await tapSize(radio)),
      label: await tapSize(decisionPrompt(page).locator('label').first()) });
    const record = decisionPrompt(page).locator('button[type="submit"]');
    const recordOff = await record.isDisabled();
    const recordLabelBefore = (await record.innerText()).trim();
    const hint = (await decisionPrompt(page).getByTestId('decision-prompt-hint').innerText()).trim();
    await record.tap({ force: true });
    // Deliberate: the prompt must STAY open and nothing may be recorded, and
    // an absence has no state to wait for, so the time is the evidence.
    await page.waitForTimeout(500);
    const stillOpen = await decisionPrompt(page).isVisible();
    const recordedByHastyTap = await completedRowFor(page, b1).isVisible().catch(() => false);
    row({ step: 'kiken', action: 'Record with no side picked', variant: 'V4 hasty (inline prompt)',
      sidePreselected: preselected > 0, recordDisabled: recordOff, label: recordLabelBefore, hint, promptStillOpen: stillOpen,
      recorded: recordedByHastyTap, shot: await shot(page, 'kiken-record-without-side') });
    expect(preselected).toBe(0);
    expect(recordOff).toBe(true);
    expect(stillOpen).toBe(true);
    expect(recordedByHastyTap).toBe(false);
    // What the operator must do: pick a side. The hasty one takes the first
    // pill offered (Shiro) without reading it, though the intended withdrawer
    // is Aka; the Record button and the line above it now name the side. The
    // wrong side is what the fusensho chain and the undo below then work from.
    const pickedSide = 'shiro';
    await decisionPrompt(page).locator(`input[name="decision-side"][value="${pickedSide}"]`).tap();
    const recordLabel = (await record.innerText()).trim();
    const consequence = (await decisionPrompt(page).getByTestId('decision-prompt-consequence').innerText()).trim();
    await shot(page, 'kiken-first-side-picked');
    await record.tap();
    // Recording a withdrawal changes only that match: no panel offers the
    // competitor's other matches.
    const withdrawnName = b1[pickedSide];
    row({ step: 'kiken', action: 'pick the first side offered, then Record', variant: 'V4 hasty (inline prompt)',
      intendedWithdrawer: `aka: ${b1.aka}`, pickedSide, recordLabel, consequence, recordedWithdrawer: withdrawnName,
      wrongSide: pickedSide !== 'aka', shot: await shot(page, 'kiken-recorded-first-side') });
    expect(recordLabel).toBe('Record: SHIRO withdrew');
    const kikenRow = completedRowFor(page, b1);
    await expect(kikenRow).toBeVisible();
    const kikenResult = (await kikenRow.locator('.shiaijo-qrow__result').innerText()).trim();
    // Docs: the winner shows two circles, Kiken sits beside the withdrawer.
    expect(kikenResult).toMatch(/○/);
    expect(kikenResult).toMatch(/Kiken/);

    // The withdrawn competitor's other matches are closed one at a time, each
    // from its own notice. The operator looks up at the shiaijo for a moment
    // first: nothing is lost by it.
    await page.waitForTimeout(3000);
    row({ step: 'kiken chain', action: 'the console, 3s after the withdrawal', variant: 'V3 interrupt (looked away)',
      editor: await inlineEditor(page).isVisible().catch(() => false),
      idle: await page.locator('.shiaijo__placeholder').isVisible().catch(() => false),
      shot: await shot(page, 'kiken-chain-after-3s') });

    // The withdrawn competitor's next bout is still in this court's queue,
    // now carrying the barred-match notice (bc-tmfn): Start match is gone
    // from that row, replaced by the notice's own one-tap resolution.
    const nextOfWithdrawn = page.locator(`${UP_NEXT}, .shiaijo__queue .shiaijo-qrow:not(.shiaijo-qrow--complete)`)
      .filter({ hasText: withdrawnName }).first();
    await expect(nextOfWithdrawn).toBeVisible();
    const pairW = await sides(nextOfWithdrawn);
    const notice = nextOfWithdrawn.getByTestId('barred-match-notice');
    await expect(notice).toBeVisible();
    const noticeText = (await notice.locator('div').first().innerText()).trim();
    const startCount = await nextOfWithdrawn.getByRole('button', { name: 'Start match' }).count();
    row({ step: 'kiken chain', action: "The withdrawn competitor's next bout (court console)", variant: 'correct path',
      pair: pairW, noStartButton: startCount === 0, notice: noticeText,
      shot: await shot(page, 'withdrawn-next-bout-barred') });
    expect(startCount).toBe(0);
    expect(noticeText).toBe(`${withdrawnName} withdrew: record the fusensho.`);

    // Record the fusensho the way the operator does: tap the notice's own
    // button, then double-tap it (the impatient thumb) -- it locks itself on
    // the first response, so only one decision must land.
    const fusenshoBtn = notice.getByTestId('barred-match-record-fusensho');
    row({ step: 'kiken chain', action: 'Record fusensho for the opponent (barred-match notice)', variant: 'size', ...(await tapSize(fusenshoBtn)) });
    const dblFusensho = await doubleTap(fusenshoBtn);
    const chainRow = completedRowFor(page, pairW);
    await expect(chainRow).toBeVisible({ timeout: 8000 });
    const chainResult = (await chainRow.locator('.shiaijo-qrow__result').innerText()).trim();
    row({ step: 'kiken chain', action: 'Record fusensho for the opponent (barred-match notice)', variant: 'V2 doubleTap', ...dblFusensho,
      result: chainResult, cardLeftQueue: !(await nextOfWithdrawn.isVisible().catch(() => false)),
      shot: await shot(page, 'chain-fusensho-recorded') });
    // The notice records a fusensho: the winner shows two circles and the
    // Fus. mark sits beside them, the side that was present (sideMarks).
    expect(chainResult).toMatch(/○/);
    expect(chainResult).toMatch(/Fus\./);

    // The undo, performed the way it happens: the court has already moved on
    // (Up next started), then the operator realises Aka withdrew, not Shiro.
    await openShiaijo(page, 'E');
    const moved = await ensureRunning(page);
    expect(moved, 'the court moves on to the Up next bout').not.toBeNull();
    row({ step: 'kiken undo', action: 'court moves on: Up next started', variant: 'correct path', running: moved,
      involvesRealWithdrawer: !!moved && (moved.shiro === b1.aka || moved.aka === b1.aka) });
    // Correct is refused while that bout runs (bc-crpn): send it back first.
    const undoOpen = await correctCompleted(page, b1);
    row({ step: 'kiken undo', action: 'Correct the kiken while the next bout runs', variant: 'correct path (refused, bc-crpn)',
      liveBout: undoOpen.live, ...undoOpen.refusal });
    await shot(page, 'kiken-correction-open');
    const undo = await recordDecision(page, 'kiken-voluntary', { side: 'aka', reason: 'wrong side recorded' });
    // Docs: the kiken now sits beside Aka, the real withdrawer. No confirm
    // asks first any more (bc-dlck): the correction changes only this match.
    const undoneRow = completedRowFor(page, b1).locator('.shiaijo-qrow__result');
    await expect.poll(async () => (await undoneRow.innerText()).trim(), { timeout: 8000 }).toMatch(/○○ vs Kiken|○○.*Kiken$/);
    const undoneResult = (await undoneRow.innerText()).trim();
    const confirmShown = await page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).isVisible().catch(() => false);
    row({ step: 'kiken undo', action: 'Kiken - Voluntary on the other side (correction)', variant: 'correct path (no confirm, bc-dlck)',
      ...undo, confirmShown, result: undoneResult, shot: await shot(page, 'kiken-undo-recorded') });
    expect(confirmShown).toBe(false);
    const backToCourt = page.getByRole('button', { name: /Back to court/ });
    if (await backToCourt.isVisible().catch(() => false)) {
      await backToCourt.tap();
      await expect(backToCourt).toBeHidden();
    }
    const liveAfter = await inlineRunning(page) ? await editorSides(page) : null;
    row({ step: 'kiken undo', action: 'back to the court after the undo', variant: 'V5 lost place',
      running: liveAfter, runningHasNowIneligible: !!liveAfter && (liveAfter.shiro === b1.aka || liveAfter.aka === b1.aka),
      shot: await shot(page, 'after-undo-court-E') });
    await page.goto(`/admin/competition/${comps.F1b}/pools`);
    await shot(page, 'after-undo-pools-tab', { fullPage: true });

    // Pool B on court E: a fusenpai with Record double-tapped, then the
    // auto-advance to the next bout on the same court.
    await openShiaijo(page, 'E');
    if (await inlineRunning(page)) {
      // The bout left running with the now-withdrawn competitor: the
      // operator scores it for the other side and Finishes.
      const live = await editorSides(page);
      const opp = live.aka === b1.aka ? 'shiro' : 'aka';
      // A toast left by an earlier step must not stand for this Finish's answer:
      // let it go, so what shows next is this tap's.
      await expect(page.locator('.toast.is-visible')).toHaveCount(0);
      await ipponButton(page, opp, 'M').tap();
      await settle(page);
      await (await armFinish(page)).tap();
      await expect(completedRowFor(page, live).or(page.locator('.toast.is-visible, .pending-write-banner').first())).toBeVisible();
      const msg = (await page.locator('.toast.is-visible, .pending-write-banner').allInnerTexts().catch(() => [])).join(' | ');
      const done = await completedRowFor(page, live).isVisible().catch(() => false);
      row({ step: 'kiken undo', action: 'Finish the running bout of the now-withdrawn competitor (M for the opponent)', variant: 'recovery attempt',
        pair: live, completed: done, message: msg.replace(/\s+/g, ' ').slice(0, 240), shot: await shot(page, 'finish-bout-of-withdrawn') });
      if (!done && await inlineRunning(page)) {
        await page.getByRole('button', { name: 'Send back to queue' }).tap();
        await hastyShiaijoConfirm(page);
      }
    }
    const poolBCard = page.locator(`${UP_NEXT}, .shiaijo__queue .shiaijo-qrow:not(.shiaijo-qrow--complete)`)
      .filter({ hasText: /Match 1 of 3/ }).filter({ hasNotText: withdrawnName }).last();
    const b2 = await sides(poolBCard);
    await poolBCard.getByRole('button', { name: 'Start match' }).tap();
    await expect.poll(() => inlineRunning(page), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    expect(await editorSides(page)).toEqual(b2);
    const id2 = await editorIdentity(page);
    await decisionButton(page, 'fusenpai').tap();
    await expect(decisionPrompt(page)).toBeVisible();
    await decisionPrompt(page).locator('input[name="decision-side"][value="aka"]').check();
    const dblRecord = await doubleTap(decisionPrompt(page).getByRole('button', { name: 'Record' }));
    // Either a confirm asks first, or the decision is recorded.
    const confirmDialog = page.locator('.modal[role="dialog"]');
    await expect(confirmDialog.first().or(completedRowFor(page, b2))).toBeVisible();
    const dialogAfterDbl = await confirmDialog.first().isVisible().catch(() => false);
    let dblDialog = null;
    if (dialogAfterDbl) {
      await shot(page, 'fusenpai-double-record-dialog');
      dblDialog = await hastyConfirm(page);
    }
    const fusRow = completedRowFor(page, b2);
    await expect(fusRow).toBeVisible();
    const fusResult = (await fusRow.locator('.shiaijo-qrow__result').innerText()).trim();
    // Recording it starts the court's next bout in the same competition, and
    // skips every bout with a barred side. Two of the bouts left on E have
    // one: the real withdrawer's (b1.aka) remaining pool A bout and the
    // no-show's (b2.aka) other pool B bout. That leaves exactly one to start,
    // pool B's bout between the side that was present and the third member.
    const barred = [b1.aka, b2.aka];
    let advancedTo = null;
    const advance = await expect.poll(async () => {
      if (!(await inlineRunning(page))) return 'idle';
      const now = await editorSides(page);
      if (now.shiro === b2.shiro && now.aka === b2.aka) return 'same';
      advancedTo = now;
      return 'moved';
    }, { timeout: 8000 }).toBe('moved').then(() => null, (e) => e);
    const id3 = advancedTo ? await editorIdentity(page) : null;
    row({ step: 'fusenpai', action: 'Record (fusenpai, Aka did not show)', variant: 'V2 doubleTap', ...dblRecord,
      from: id2.eyebrow, result: fusResult, dialogAfterDoubleTap: dialogAfterDbl, dialog: dblDialog,
      advancedTo: id3 && id3.eyebrow, court: id3 && id3.court, shot: await shot(page, 'fusenpai-recorded') });
    // Docs: the winner shows two circles and Fus. sits beside the no-show.
    expect(fusResult).toMatch(/○/);
    expect(fusResult).toMatch(/Fus\./);
    if (advance) throw advance;
    expect(id3.court).toBe('E');
    expect(id3.eyebrow).toMatch(/POOL B/);
    expect(barred, 'the bout started has no barred side').not.toContain(advancedTo.shiro);
    expect(barred).not.toContain(advancedTo.aka);

    // Knockout (F2, court C): a tied 1-1 semi-final, encho, hantei.
    await openShiaijo(page, 'C');
    const semi = await ensureRunning(page);
    expect(semi, 'a semi-final starts on C').not.toBeNull();
    expect((await editorIdentity(page)).eyebrow).toMatch(/SEMIFINAL/);
    await ipponButton(page, 'shiro', 'M').tap();
    await ipponButton(page, 'aka', 'K').tap();
    await settle(page);
    const markDraw = inlineEditor(page).getByTestId('scoring-modal-mark-draw');
    row({ step: 'knockout tie', action: 'Mark draw', variant: 'correct path', disabled: await markDraw.isDisabled(),
      title: await markDraw.getAttribute('title') });
    // V1-like: the thumb reaches for Finish on a 1-1 knockout bout. The
    // button itself now refuses the tie (bc-tmfn): "Needs a winner",
    // disabled, rather than an enabled Finish the server would reject after
    // the two-tap confirm.
    const koTieBtn = inlineEditor(page).locator('button').filter({ hasText: 'Needs a winner' }).first();
    const koTieDisabled = await koTieBtn.isDisabled();
    const koTieTitle = await koTieBtn.getAttribute('title');
    const koTieLabel = (await koTieBtn.innerText()).trim();
    const tieCompleted = await completedRowFor(page, semi).isVisible().catch(() => false);
    row({ step: 'knockout tie', action: 'Finish on a 1-1 knockout bout', variant: 'V1 (Finish instead of Overtime)',
      finishEnabled: !koTieDisabled, label: koTieLabel, title: koTieTitle, completed: tieCompleted,
      stillRunning: await inlineRunning(page), shot: await shot(page, 'finish-on-knockout-tie') });
    expect(koTieDisabled).toBe(true);
    expect(tieCompleted, 'the tied bout is not completed').toBe(false);
    expect(koTieLabel).toBe('Needs a winner');
    expect(koTieTitle).toBe('Needs a winner: fight encho, then record hantei if still tied.');
    expect(tieCompleted).toBe(false);

    const pill = inlineEditor(page).getByTestId('scoring-modal-encho-pill');
    row({ step: 'encho', action: 'Overtime pill', variant: 'size', ...(await tapSize(pill)) });
    // Docs: "open the Overtime control and tick Encho started". The pill is
    // tapped where a thumb lands, its centre.
    await pill.tap();
    const encho = inlineEditor(page).getByTestId('scoring-modal-encho-checkbox');
    await expect(encho).toBeVisible();
    const openedByCentre = await encho.isVisible().catch(() => false);
    const enchoPopover = await page.getByText('Overtime, when a knockout match').isVisible().catch(() => false);
    row({ step: 'encho', action: 'tap the Overtime pill (centre)', variant: 'correct path', counterOpened: openedByCentre,
      glossaryPopoverOpened: enchoPopover, shot: await shot(page, 'overtime-pill-tapped') });
    // The word itself opens the counter (bc-otpl); the "?" beside it is the glossary.
    expect(openedByCentre).toBe(true);
    expect(enchoPopover).toBe(false);
    row({ step: 'encho', action: 'Encho started checkbox', variant: 'size', ...(await tapSize(encho)),
      label: await tapSize(inlineEditor(page).locator('.encho-row__label')) });
    await encho.check();
    const plus = inlineEditor(page).getByRole('button', { name: 'Increase overtime period count' });
    const minus = inlineEditor(page).getByRole('button', { name: 'Decrease overtime period count' });
    const count = () => inlineEditor(page).locator('.encho-row__count').innerText();
    row({ step: 'encho', action: '+ / -', variant: 'size', plus: await tapSize(plus), minus: await tapSize(minus) });
    const minusAtOne = await minus.isDisabled();
    for (let i = 0; i < 6; i += 1) await plus.tap();
    const afterSix = await count();
    const dblPlus = await doubleTap(plus);
    const afterDblPlus = await count();
    for (let i = 0; i < 12; i += 1) if (await minus.isEnabled()) await minus.tap();
    const floor = await count();
    const minusDisabledAtFloor = await minus.isDisabled();
    await plus.tap();
    row({ step: 'encho', action: '+ x6, double tap +, - to the floor', variant: 'V2 doubleTap (+)', minusDisabledAtOne: minusAtOne,
      afterSixPlus: afterSix, ...dblPlus, afterDoubleTapPlus: afterDblPlus, floor, minusDisabledAtFloor,
      shot: await shot(page, 'encho-counter') });
    expect(afterSix).toBe('×7');
    expect(floor).toBe('×1');
    expect(minusDisabledAtFloor).toBe(true);
    await settle(page);

    // Hantei: arm, then the thumb lands on the neighbour of SHIRO wins. A
    // side button only picks (bc-htsd); Finish (two taps) records the pick.
    await inlineEditor(page).getByTestId('scoring-modal-hantei-arm').tap();
    const shiroWins = inlineEditor(page).getByTestId('scoring-modal-hantei-shiro');
    const akaWins = inlineEditor(page).getByTestId('scoring-modal-hantei-aka');
    row({ step: 'hantei', action: 'SHIRO wins / AKA wins', variant: 'size', shiro: await tapSize(shiroWins), aka: await tapSize(akaWins),
      labels: [(await shiroWins.innerText()).trim(), (await akaWins.innerText()).trim()] });
    await shot(page, 'hantei-armed');
    const nbHantei = await tapNeighbour(shiroWins, 'right');
    // The tap picked a side (a positive signal) before it is read that nothing committed.
    await expect.poll(async () => (await shiroWins.getAttribute('aria-pressed')) === 'true'
      || (await akaWins.getAttribute('aria-pressed')) === 'true', { message: 'the neighbour tap picked a side' }).toBe(true);
    const semiRow = completedRowFor(page, semi);
    const committedByPick = await semiRow.isVisible().catch(() => false);
    const picked = { shiro: await shiroWins.getAttribute('aria-pressed'), aka: await akaWins.getAttribute('aria-pressed') };
    row({ step: 'hantei', action: 'SHIRO wins (intended)', variant: 'V1 tapNeighbour right', hit: nbHantei.hit.label, gapPx: nbHantei.gapPx,
      picked, committedByPick, finishLabel: (await finishButton(page).innerText()).trim(), shot: await shot(page, 'hantei-neighbour-tapped') });
    expect(committedByPick).toBe(false);
    // The hasty operator Finishes without reading which side is picked.
    await finishMatch(page, INLINE_EDITOR);
    const committed = await semiRow.waitFor({ state: 'visible', timeout: 6000 }).then(() => true, () => false);
    const firstRead = committed ? (await semiRow.locator('.shiaijo-qrow__result').innerText()).trim() : null;
    const updatedInPlace = await expect.poll(async () => (await semiRow.locator('.shiaijo-qrow__result').innerText()).trim(), { timeout: 6000 })
      .toMatch(/Ht/).then(() => true, () => false);
    await page.reload();
    await expect(semiRow).toBeVisible();
    const wrongResult = (await semiRow.locator('.shiaijo-qrow__result').innerText()).trim();
    row({ step: 'hantei', action: 'the recorded hantei result, read before and after a reload', variant: 'correct path',
      firstRead, htShownWithin6sWithoutReload: updatedInPlace, afterReload: wrongResult, shot: await shot(page, 'hantei-result-after-reload') });
    row({ step: 'hantei', action: 'Finish (two taps) on the neighbour\'s pick', variant: 'V4 hasty (did not read the pick)',
      picked, committed, result: wrongResult });
    // The recorded result reads as the docs describe: the winner's points,
    // Ht beside the winner (the side the neighbour tap picked), (E) in the centre.
    expect(htSide(wrongResult)).toBe(picked.aka === 'true' ? 'aka' : 'shiro');
    expect(wrongResult).toMatch(/\(E\)/);

    // Recovery: Correct the semi-final and give hantei to Shiro. Finish +
    // Start Next has started the other semi-final, so Correct is refused
    // until it is sent back (bc-crpn).
    const htOpen = await correctCompleted(page, semi);
    row({ step: 'hantei', action: 'Correct the hantei while the next semi-final runs', variant: 'correct path (refused, bc-crpn)',
      liveBout: htOpen.live, ...htOpen.refusal });
    await shot(page, 'hantei-correction-open');
    const armAgain = inlineEditor(page).getByTestId('scoring-modal-hantei-arm');
    if (await armAgain.isVisible().catch(() => false)) await armAgain.tap();
    await inlineEditor(page).getByTestId('scoring-modal-hantei-shiro').tap();
    await inlineEditor(page).getByRole('button', { name: 'Save correction' }).tap();
    const reasonPrompt = inlineEditor(page).locator('.reason-prompt');
    const reasonAsked = await reasonPrompt.waitFor({ state: 'visible', timeout: 4000 }).then(() => true, () => false);
    if (reasonAsked) await reasonPrompt.locator('button.btn--primary').tap();
    // The verdict moved to Shiro: Ht now sits in Shiro's cell, still with (E).
    await expect.poll(async () => htSide((await semiRow.locator('.shiaijo-qrow__result').innerText()).trim()), { timeout: 10_000 })
      .toBe('shiro');
    // The refusal's own toast may still be up; only a toast the save raised counts.
    const toast = (await page.locator('.toast').allInnerTexts().catch(() => []))
      .filter((t) => t !== htOpen.refusal?.toast).join(' | ');
    const fixed = (await semiRow.locator('.shiaijo-qrow__result').innerText()).trim();
    row({ step: 'hantei', action: 'Correct -> SHIRO wins -> Save correction', variant: 'recovery', reasonAsked, toast, result: fixed,
      shot: await shot(page, 'hantei-corrected') });
    expect(htSide(fixed)).toBe('shiro');
    expect(fixed).toMatch(/\(E\)/);
  });
  // J6. Confirm + advance OFFLINE (F2, court C): the second semi-final is
  // finished with the network down. The write is queued, the local bracket
  // advances; back online, the server must agree.
  test('J6 finish a knockout bout offline, then reconnect', async ({ page, context, browser, baseURL }) => {
    test.setTimeout(180_000);
    const J = 'J6';
    const shot = journeyShots('knockout-mixed-individual/J6');
    const row = (r) => audit.record({ journey: J, ...r });
    await login(page);
    await openShiaijo(page, 'C');
    // Semi-final 1 (M1) online first unless J2 already played it.
    await expect.poll(async () => (await inlineRunning(page)) || (await upNextCard(page).isVisible().catch(() => false)), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    let played = null;
    const liveNow = (await inlineRunning(page)) ? (await editorIdentity(page)).eyebrow : '';
    const upNow = ((await page.locator('.shiaijo-upnext__time').allInnerTexts())[0] || '');
    if (/MATCH 1\b/.test(liveNow) || (!liveNow && /Match 1\b/.test(upNow))) played = await playRunningBout(page);
    // Start the remaining semi-final from Up next unless it is already live.
    let semi = null;
    await expect.poll(async () => {
      if (await inlineRunning(page)) {
        const now = await editorSides(page);
        if (!played || now.shiro !== played.shiro || now.aka !== played.aka) { semi = now; return 'running'; }
      }
      if (await page.locator('.shiaijo__placeholder').isVisible().catch(() => false)) return 'idle';
      return 'waiting';
    }, { timeout: EXPECT_TIMEOUT_MS }).not.toBe('waiting');
    if (!semi) {
      semi = await sides(upNextCard(page));
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      await expect.poll(async () => ((await inlineRunning(page)) ? JSON.stringify(await editorSides(page)) : ''), { timeout: EXPECT_TIMEOUT_MS })
        .toBe(JSON.stringify(semi));
    }
    row({ step: 'finish offline', action: 'the last semi-final is running', variant: 'correct path', semi });
    expect((await editorIdentity(page)).eyebrow).toMatch(/SEMIFINAL/);
    const w = winnerOf(semi);
    const winnerName = semi[w];
    const loserName = semi[w === 'shiro' ? 'aka' : 'shiro'];
    await ipponButton(page, w, 'M').tap();
    await settle(page);

    // Network down; Finish (two taps).
    await context.setOffline(true);
    await (await armFinish(page)).tap();
    await page.waitForTimeout(300);
    const at300 = {
      editor: await inlineEditor(page).isVisible().catch(() => false),
      banner: (await page.locator('.pending-write-banner').allInnerTexts()).join(' | '),
      upNext: await sides(upNextCard(page)).catch(() => null),
      status: ((await page.locator('.admin-conn, [aria-label*="real-time"], [role="status"]').allInnerTexts().catch(() => [])) || []).join(' | ').slice(0, 120),
    };
    row({ step: 'finish offline', action: 'the screen 300ms after Finish, offline', variant: 'V5 lost place', ...at300,
      shot: await shot(page, 'offline-finish-300ms') });
    // The operator watches the screen for the rest of two seconds with the
    // network down: what it shows by then (a banner, or the court moved on) is
    // what the row records, so the time is the scenario, not a wait for the app.
    await page.waitForTimeout(1700);
    const banner = inlineEditor(page).locator('.pending-write-banner');
    const bannerText = (await banner.allInnerTexts()).join(' | ');
    const editorStill = await inlineEditor(page).isVisible().catch(() => false);
    const editorId = editorStill ? await editorIdentity(page) : null;
    const pill = editorStill ? ((await syncState(page).allInnerTexts())[0] || '').trim() : null;
    const finalRow = page.locator('.shiaijo__queue').getByText(/Veterans Knockout · Match 3\b/).locator(`xpath=ancestor::*[contains(@class,"shiaijo-qrow") or contains(@class,"${UP_NEXT.slice(1)}")][1]`);
    const finalText = (await finalRow.allInnerTexts()).join(' ').replace(/\s+/g, ' ');
    const queueText = (await page.locator('.shiaijo__queue').innerText()).replace(/\s+/g, ' ');
    row({ step: 'finish offline', action: 'Finish (two taps) with the network down', variant: 'V3 interrupt offline',
      banner: bannerText, editorStillOnBout: editorId && editorId.eyebrow, syncPill: pill,
      finalShowsWinnerLocally: finalText.includes(winnerName), finalRow: finalText.slice(0, 160),
      bronzeMentionsLoser: /3rd|Bronze/i.test(queueText) && queueText.includes(loserName),
      shot: await shot(page, 'offline-finished') });
    await shot(page, 'offline-finished-full', { fullPage: true });

    // V2: the impatient operator taps "Retry now" (and again) while offline.
    const retry = banner.getByRole('button', { name: /Retry/ });
    let retryRow = { present: await retry.isVisible().catch(() => false) };
    if (retryRow.present) {
      retryRow = { ...retryRow, size: await tapSize(retry), ...(await doubleTap(retry)) };
      // Deliberate: a settle before an audit row only; nothing is asserted on it.
      await page.waitForTimeout(1500);
      retryRow.bannerAfter = (await banner.allInnerTexts()).join(' | ');
    }
    row({ step: 'finish offline', action: 'Retry now while offline', variant: 'V2 doubleTap', ...retryRow,
      shot: await shot(page, 'offline-retry-double-tapped') });

    // V5: the operator, unsure whether Finish landed, sees the bout offered
    // again in Up next and taps Start match (still offline).
    const upAgain = await sides(upNextCard(page)).catch(() => null);
    let restart = null;
    if (upAgain && upAgain.shiro === semi.shiro && upAgain.aka === semi.aka) {
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      // Deliberate: a settle before an audit row only; nothing is asserted on it.
      await page.waitForTimeout(1500);
      restart = { editor: await inlineEditor(page).isVisible().catch(() => false),
        running: await inlineRunning(page) ? await editorSides(page) : null,
        banner: (await page.locator('.pending-write-banner').allInnerTexts()).join(' | ') };
    }
    row({ step: 'finish offline', action: 'Start match on the same bout, offered again in Up next (offline)', variant: 'V5 lost place',
      upNextWasTheFinishedBout: !!restart, ...(restart || {}), shot: await shot(page, 'offline-restart-same-bout') });

    // V3: the iPad reloads the tab while still offline.
    const reloadOffline = await page.reload({ timeout: 10_000 }).then(() => 'loaded', (e) => `failed: ${String(e.message).split('\n')[0].slice(0, 80)}`);
    row({ step: 'finish offline', action: 'reload while offline', variant: 'V3 interrupt reload (offline)', reload: reloadOffline,
      shot: await shot(page, 'offline-reload') });

    // Back online.
    await context.setOffline(false);
    await page.goto('/admin/shiaijo/C');
    await expect(page.getByRole('heading', { level: 1, name: 'Shiaijo C' })).toBeVisible();
    const landed = await expect(completedRowFor(page, semi)).toBeVisible({ timeout: 20_000 }).then(() => true, () => false);
    const semiResult = landed ? (await completedRowFor(page, semi).locator('.shiaijo-qrow__result').innerText()).trim() : null;
    const courtNow = { running: await inlineRunning(page) ? await editorSides(page) : null,
      upNext: await sides(upNextCard(page)).catch(() => null) };
    row({ step: 'reconnect', action: 'back online, reopen the court', variant: 'V5 lost place', semiLanded: landed, semiResult, ...courtNow,
      shot: await shot(page, 'online-again') });
    expect(landed).toBe(true);
    expect(semiResult).toMatch(/M/);

    // The first device's held write has gone out: its topbar shows no held-writes
    // count once the queue is empty.
    await expect(page.getByTestId('topbar-held')).toHaveCount(0, { timeout: 20_000 });

    // A second device (its own browser context, so its own storage and no
    // outbox shared with the first) reads the server's bracket.
    const otherCtx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL });
    try {
      const other = await otherCtx.newPage();
      await login(other);
      await other.goto(`/admin/competition/${comps.F2}/bracket`);
      await expect(other.getByText('3RD PLACE MATCH')).toBeVisible();
      // The server's bracket seats the semi-final's loser in the 3rd-place match.
      const bracketText = (await other.locator('.page').first().innerText()).replace(/\s+/g, ' ');
      await expect.poll(async () => ((await other.locator('.page').first().innerText()).replace(/\s+/g, ' ').split('3RD PLACE MATCH')[1] || ''),
        { timeout: EXPECT_TIMEOUT_MS }).toContain(loserName);
      row({ step: 'reconnect', action: 'second device reads the bracket', variant: 'correct path',
        finalHasWinner: bracketText.includes(winnerName), bronzeHasLoser: (bracketText.split('3RD PLACE MATCH')[1] || '').includes(loserName),
        shot: await shot(other, 'server-bracket-after-reconnect', { fullPage: true }) });
    } finally {
      await otherCtx.close();
    }
  });

  // J8. The same scoring from the competition's own Pools tab (F1c, overlay
  // editor) and Bracket tab (F2d, inline running-match panel). Neither has
  // Finish + Start Next: after Finish, where does the operator stand?
  test('J8 scoring from the Pools tab and the Bracket tab', async ({ page }) => {
    test.setTimeout(180_000);
    const J = 'J8';
    const shot = journeyShots('knockout-mixed-individual/J8');
    const row = (r) => audit.record({ journey: J, ...r });
    await login(page);

    // Pools tab.
    await page.goto(`/admin/competition/${comps.F1c}/pools`);
    const firstRow = page.locator('.pool-match-numbered-list > *').first();
    await expect(firstRow).toBeVisible();
    const rowText = (await firstRow.innerText()).replace(/\s+/g, ' ');
    row({ step: 'pools tab', action: 'match row (open the editor)', variant: 'size', ...(await tapSize(firstRow)) });
    await firstRow.tap();
    await expect(page.locator(EDITOR)).toBeVisible();
    const pair = await editorSides(page, EDITOR);
    const idP = await editorIdentity(page, EDITOR);
    await shot(page, 'pools-tab-editor-open');
    const start = page.locator(EDITOR).getByRole('button', { name: 'Start match' });
    const startOffered = await start.isVisible().catch(() => false);
    if (startOffered) await startMatch(page);
    const openAfterStart = await page.locator(EDITOR).isVisible().catch(() => false);
    row({ step: 'pools tab', action: 'Start match in the Pools-tab editor', variant: 'correct path', row: rowText.slice(0, 80),
      startOffered, editorStillOpen: openAfterStart, shot: await shot(page, 'pools-tab-after-start') });
    if (!openAfterStart) {
      await firstRow.tap();
      await expect(page.locator(EDITOR)).toBeVisible();
    }
    const w = winnerOf(pair);
    const ipponSaved = nextTapWrite(page);
    await ipponButton(page, w, 'M', EDITOR).tap();
    await writeLanded(ipponSaved, 'the ippon save');
    const openAfterIppon = await page.locator(EDITOR).isVisible().catch(() => false);
    row({ step: 'pools tab', action: 'award one ippon in the Pools-tab editor', variant: 'correct path', editorStillOpen: openAfterIppon,
      shot: await shot(page, 'pools-tab-after-ippon') });
    if (!openAfterIppon) {
      await firstRow.tap();
      await expect(page.locator(EDITOR)).toBeVisible();
      row({ step: 'pools tab', action: 'reopen after the editor closed itself', variant: 'recovery', taps: 1,
        marksKept: await slotMarks(page, w, EDITOR) });
    }
    const finishLabel = (await finishButton(page, EDITOR).innerText()).trim();
    await finishMatch(page, EDITOR);
    // Where the screen ends up once the list has refetched after the finish.
    const after = await untilStable(async () => ({
      editorOpen: await page.locator(EDITOR).isVisible().catch(() => false),
      url: new URL(page.url()).pathname,
      rowNow: (await firstRow.innerText()).replace(/\s+/g, ' ').slice(0, 120),
      toast: (await page.locator('.toast').allInnerTexts().catch(() => [])).join(' | '),
    }));
    row({ step: 'pools tab', action: 'Finish (Pools tab: no Start Next)', variant: 'V5 lost place', finishLabel, court: idP.court, ...after,
      shot: await shot(page, 'pools-tab-after-finish') });
    expect(after.editorOpen).toBe(false);
    expect(after.rowNow).toMatch(/M/);

    // Pools tab, V4: the second row opened, one ippon, then dismissed hastily.
    const secondRow = page.locator('.pool-match-numbered-list > *').nth(1);
    await secondRow.tap();
    await expect(page.locator(EDITOR)).toBeVisible();
    const pair2 = await editorSides(page, EDITOR);
    if (await page.locator(EDITOR).getByRole('button', { name: 'Start match' }).isVisible().catch(() => false)) {
      await startMatch(page);
      if (!(await page.locator(EDITOR).isVisible().catch(() => false))) await secondRow.tap();
    }
    await ipponButton(page, winnerOf(pair2), 'M', EDITOR).tap();
    // Close straight away (inside the autosave's 300ms), as a hasty operator would.
    const closedByTap = await page.locator(EDITOR).getByRole('button', { name: /✕ Close/ }).tap({ timeout: 2000 }).then(() => true, () => false);
    const discardAsked = await page.locator('.modal[role="dialog"] .modal__foot').isVisible().catch(() => false);
    // `from`: the editor as it stands before the answer (still open behind the
    // confirm), so the settle cannot agree with the page as it was before.
    const editorBeforeAnswer = await page.locator(EDITOR).isVisible().catch(() => false);
    const hasty = discardAsked ? await hastyConfirm(page) : null;
    // Whether the editor closed (and the unsaved ippon was kept) settles after the answer.
    await untilStable(() => page.locator(EDITOR).isVisible().catch(() => false), { from: editorBeforeAnswer });
    await secondRow.tap();
    await expect(page.locator(EDITOR)).toBeVisible();
    const kept = await slotMarks(page, winnerOf(pair2), EDITOR);
    row({ step: 'pools tab', action: 'Close with an unsaved ippon', variant: 'V4 hastyConfirm', closedByTap, discardAsked, ...(hasty || {}),
      marksAfterReopen: kept, shot: await shot(page, 'pools-tab-reopened') });
    await page.locator(EDITOR).getByRole('button', { name: /✕ Close/ }).tap().catch(() => {});
    if (await page.locator('.modal[role="dialog"] .modal__foot').isVisible().catch(() => false)) await hastyConfirm(page);

    // Bracket tab (F2d): a semi-final card.
    await page.goto(`/admin/competition/${comps.F2d}/bracket`);
    const card = page.getByText(/^Xan Moss$|^Yui Hara$|^Zoe Quinn$|^Ada Pike$/).first();
    await card.tap();
    const panel = page.locator('.running-panel');
    await expect(panel).toBeVisible();
    await shot(page, 'bracket-tab-panel');
    const startB = panel.getByRole('button', { name: 'Start match' });
    if (await startB.isVisible().catch(() => false)) await startB.tap();
    await expect.poll(async () => (await panel.locator('.editor-head-pill').count()), { timeout: 10_000 }).toBe(0);
    const bpair = await editorSides(page);
    await ipponButton(page, winnerOf(bpair), 'M').tap();
    await settle(page);
    await finishMatch(page, INLINE_EDITOR);
    await expect(panel.locator('.running-panel__result')).toBeVisible();
    const bAfter = {
      resultCard: await panel.locator('.running-panel__result').isVisible().catch(() => false),
      editRes: await panel.getByRole('button', { name: 'Edit result' }).isVisible().catch(() => false),
      editor: await panel.locator(INLINE_EDITOR).isVisible().catch(() => false),
    };
    row({ step: 'bracket tab', action: 'Finish (Bracket tab: no Start Next)', variant: 'V5 lost place', pair: bpair, ...bAfter,
      shot: await shot(page, 'bracket-tab-after-finish', { fullPage: true }) });
    expect(bAfter.resultCard).toBe(true);

    // V4: Edit result, answered hastily.
    row({ step: 'bracket tab', action: 'Edit result', variant: 'size', ...(await tapSize(panel.getByRole('button', { name: 'Edit result' }))) });
    await panel.getByRole('button', { name: 'Edit result' }).tap();
    const editDialog = await hastyConfirm(page);
    // The reopen is a server write: the editor is read once it has left the
    // result card's state (not shown), or settled on not shown if it never did.
    const editorReopened = await untilStable(() => panel.locator(INLINE_EDITOR).isVisible().catch(() => false), { from: false });
    row({ step: 'bracket tab', action: 'Edit result confirm', variant: 'V4 hastyConfirm', ...editDialog,
      editorReopened,
      shot: await shot(page, 'bracket-tab-edit-result') });
    const closeEdit = panel.getByRole('button', { name: /✕ Close|^Cancel$/ }).first();
    if (await closeEdit.isVisible().catch(() => false)) await closeEdit.tap();

    // The other semi-final from its bracket card; the default (two joint 3rd
    // places) has no bronze match, so the final is the last bout.
    const other = bpair.shiro === 'Zoe Quinn' || bpair.aka === 'Zoe Quinn' || bpair.shiro === 'Ada Pike' || bpair.aka === 'Ada Pike'
      ? /^Xan Moss$/ : /^Zoe Quinn$/;
    await page.locator('.page').getByText(other).first().tap();
    await expect(panel.locator(INLINE_EDITOR)).toBeVisible();
    const startB2 = panel.getByRole('button', { name: 'Start match' });
    if (await startB2.isVisible().catch(() => false)) await startB2.tap();
    await expect.poll(async () => (await panel.locator('.editor-head-pill').count()), { timeout: 10_000 }).toBe(0);
    const bpair2 = await editorSides(page);
    await ipponButton(page, winnerOf(bpair2), 'M').tap();
    await settle(page);
    const readBracket = async () => (await page.locator('.page').first().innerText()).replace(/\s+/g, ' ');
    await finishMatch(page, INLINE_EDITOR);
    // The bracket redraws once the finish has refetched. Both semi-finals are
    // played now, so the final is seated (no "Winner of ..." left): that is
    // the state the absence below is read against, not the tree from before
    // the finish, which has no 3rd place match either. Then the text it
    // settles on.
    await expect.poll(readBracket, { message: 'the final is seated once both semi-finals are played' }).not.toMatch(/Winner of /);
    const bracketText = await untilStable(readBracket);
    row({ step: 'bracket tab', action: 'second semi-final, default joint 3rd places', variant: 'correct path', pair: bpair2,
      bronzeMatchShown: /3RD PLACE MATCH/i.test(bracketText), shot: await shot(page, 'bracket-tab-semis-done', { fullPage: true }) });
    expect(bracketText).not.toMatch(/3RD PLACE MATCH/i);

    // Fusenpai auto-advance on the court console of a court where nothing is
    // barred yet (court F, F1c): recording it starts the court's next bout in
    // the same competition, on the same court.
    await openShiaijo(page, 'F');
    const fb = await ensureRunning(page);
    expect(fb, 'court F has a bout to start').not.toBeNull();
    await recordDecision(page, 'fusenpai', { side: 'aka' });
    await expect(completedRowFor(page, fb)).toBeVisible();
    const advance = await expect.poll(async () => {
      if (!(await inlineRunning(page))) return 'idle';
      const now = await editorSides(page);
      return now.shiro === fb.shiro && now.aka === fb.aka ? 'same' : 'moved';
    }, { timeout: 10_000 }).toBe('moved').then(() => null, (e) => e);
    const idF = advance ? null : await editorIdentity(page);
    row({ step: 'fusenpai (court F)', action: 'Record fusenpai on the console', variant: 'correct path (auto-advance)',
      from: fb, advanced: !advance, court: idF && idF.court, shot: await shot(page, 'fusenpai-auto-advance-F') });
    if (advance) throw advance;
    expect(idF.court).toBe('F');
    expect(idF.eyebrow).toMatch(/^CADETS INDIVIDUAL/);
  });
});

// Functional defects found by the journeys above, each now fixed and named by
// its bead. Each seeds its own competition on its own shiaijo and reads
// nothing the journeys left, so it runs alone and guards its fix. Not serial:
// a failure here, or in a journey, skips none of the others (see the header).
test.describe('knockout-mixed-individual bead guards', () => {
  const seedOwn = async (page, opts, roster) => {
    const id = await createCompetition(page, { kind: 'individual', ...opts });
    await pasteRoster(page, id, roster);
    await generateDraw(page, id);
    await startCompetition(page, id);
    return id;
  };

  test('bc-kfup: a withdrawn competitor\'s remaining pool bout can be recorded as a fusenpai', async ({ page }) => {
    await login(page);
    const id = await seedOwn(page, { name: 'Finding A2', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['H'], numberPrefix: 'FB' }, rosterOf('Atwo', 6));
    await openShiaijo(page, 'H');
    const first = await ensureRunning(page);
    expect(first, 'the first bout on H starts').not.toBeNull();
    await recordDecision(page, 'kiken-voluntary', { side: 'aka' });
    await expect(completedRowFor(page, first)).toBeVisible();
    // The withdrawn competitor's next bout, from the Scores tab.
    await page.goto(`/admin/competition/${id}/scores`);
    const next = page.locator('.score-edit-row').filter({ hasText: first.aka })
      .filter({ has: page.getByRole('button', { name: /^Score$/ }) }).first();
    await next.getByRole('button', { name: /^Score$/ }).tap();
    await expect(page.locator(EDITOR)).toBeVisible();
    const s = await editorSides(page, EDITOR);
    await recordDecision(page, 'fusenpai', { side: s.shiro === first.aka ? 'shiro' : 'aka', root: EDITOR });
    // The fusenpai landed: the row reads Fus. (a refusal would leave it unrecorded).
    await expect(page.locator('.score-edit-row').filter({ hasText: first.aka }).filter({ hasText: /Fus\./ })).toHaveCount(1);
  });

  test('bc-otpl: tapping the Overtime pill opens the encho counter, as the docs describe', async ({ page }) => {
    await login(page);
    await seedOwn(page, { name: 'Finding A3', format: 'knockout', courts: ['I'], numberPrefix: 'FC' }, rosterOf('Athree', 4));
    await openShiaijo(page, 'I');
    expect(await ensureRunning(page), 'a semi-final starts on I').not.toBeNull();
    await inlineEditor(page).getByTestId('scoring-modal-encho-pill').tap();
    await expect(inlineEditor(page).getByTestId('scoring-modal-encho-checkbox')).toBeVisible();
  });

  test('bc-htcr: a hantei verdict can be corrected to the other side from the Correct editor', async ({ page }) => {
    await login(page);
    await seedOwn(page, { name: 'Finding A4', format: 'knockout', courts: ['J'], numberPrefix: 'FD' }, rosterOf('Afour', 4));
    await openShiaijo(page, 'J');
    const semi = await ensureRunning(page);
    expect(semi, 'a semi-final starts on J').not.toBeNull();
    await ipponButton(page, 'shiro', 'M').tap();
    await ipponButton(page, 'aka', 'K').tap();
    await settle(page);
    // A side button picks the hantei winner; Finish (two taps) records it (bc-htsd).
    await inlineEditor(page).getByTestId('scoring-modal-hantei-arm').tap();
    await inlineEditor(page).getByTestId('scoring-modal-hantei-aka').tap();
    await finishMatch(page, INLINE_EDITOR);
    const done = completedRowFor(page, semi);
    await expect(done).toBeVisible();
    await expect(done.locator('.shiaijo-qrow__result')).toContainText('Ht');
    // Recorded for Aka: Ht in Aka's cell ("M vs K Ht").
    expect(htSide((await done.locator('.shiaijo-qrow__result').innerText()).trim())).toBe('aka');
    // Finish + Start Next started the other semi-final: send it back, then correct (bc-crpn).
    await correctCompleted(page, semi);
    const arm = inlineEditor(page).getByTestId('scoring-modal-hantei-arm');
    if (await arm.isVisible().catch(() => false)) await arm.tap();
    await inlineEditor(page).getByTestId('scoring-modal-hantei-shiro').tap();
    await inlineEditor(page).getByRole('button', { name: 'Save correction' }).tap();
    // Docs: a correction asks for a short reason, then applies.
    const reason = inlineEditor(page).locator('.reason-prompt');
    if (await reason.isVisible().catch(() => false)) await reason.locator('button.btn--primary').tap();
    // The verdict moved to Shiro: Ht now in Shiro's cell ("M Ht vs K").
    await expect.poll(async () => htSide((await done.locator('.shiaijo-qrow__result').innerText()).trim()), { timeout: 10_000 })
      .toBe('shiro');
  });

  test('bc-plcl: the Pools-tab score editor stays open through Start match and an ippon', async ({ page }) => {
    await login(page);
    const id = await seedOwn(page, { name: 'Finding A5', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['K'], numberPrefix: 'FE' }, rosterOf('Afive', 6));
    await page.goto(`/admin/competition/${id}/pools`);
    const first = page.locator('.pool-match-numbered-list > *').first();
    await first.tap();
    await expect(page.locator(EDITOR)).toBeVisible();
    const pair = await editorSides(page, EDITOR);
    await startMatch(page);
    await expect(page.locator(EDITOR)).toBeVisible();
    await awardIppon(page, winnerOf(pair), 'M', EDITOR);
    await expect(page.locator(EDITOR)).toBeVisible();
  });
  test('bc-sbq: Send back to queue says the score is kept, and starting the bout again carries it on', async ({ page }) => {
    await login(page);
    await seedOwn(page, { name: 'Finding A6', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['L'], numberPrefix: 'FF' }, rosterOf('Asix', 6));
    await openShiaijo(page, 'L');
    const pair = await ensureRunning(page);
    expect(pair, 'a bout starts on this court').not.toBeNull();
    // Venue wifi: the court's refresh after the tap is slow to arrive.
    // The score is on the board (and saved) all the same.
    const slowReads = async (route) => {
      if (route.request().method() !== 'GET' || route.request().url().includes('/api/events')) return route.continue();
      await new Promise((r) => setTimeout(r, 3000));
      return route.continue().catch(() => {});
    };
    await page.route('**/api/**', slowReads);
    await ipponButton(page, winnerOf(pair), 'M').tap();
    await settle(page);
    await page.getByRole('button', { name: 'Send back to queue' }).tap();
    const dialog = page.locator('.shiaijo-move-confirm[role="dialog"]');
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText('The score already saved is kept');
    await page.unroute('**/api/**', slowReads);
    await hastyShiaijoConfirm(page);
    // The dialog's promise holds: started again, the bout still has its point.
    const again = await ensureRunning(page);
    expect(again).toEqual(pair);
    await expect.poll(() => slotMarks(page, winnerOf(pair))).toEqual(['M']);
  });
  test('bc-kosc: in pools + knockout, no knockout bout is scheduled before the last pool bout', async ({ page }) => {
    await login(page);
    const id = await seedOwn(page, { name: 'Finding A7', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['M'], numberPrefix: 'FG' }, rosterOf('Aseven', 6));
    await page.goto(`/admin/competition/${id}/scores`);
    await expect(page.locator('.score-edit-row').first()).toBeVisible();
    const poolTimes = (await page.locator('.score-edit-row').allInnerTexts())
      .filter((t) => /Pool/.test(t)).map((t) => (t.match(/\d\d:\d\d/) || [])[0]).filter(Boolean).sort();
    await page.goto(`/admin/competition/${id}/bracket`);
    await expect(page.getByText(/semifinals|final/i).first()).toBeVisible();
    const bracketText = (await page.locator('.page').first().innerText()).replace(/\s+/g, ' ');
    const koTimes = [...bracketText.matchAll(/M\d+ SHIAIJO \S+ (\d\d:\d\d)/g)].map((m) => m[1]).sort();
    expect(poolTimes.length).toBeGreaterThan(0);
    expect(koTimes.length).toBeGreaterThan(0);
    expect(koTimes[0] > poolTimes[poolTimes.length - 1]).toBe(true);
  });
  test('bc-offl: finishing a bout offline on the court console says the result is not saved yet', async ({ page, context }) => {
    await login(page);
    await seedOwn(page, { name: 'Finding A8', format: 'knockout', courts: ['N'], numberPrefix: 'FH' }, rosterOf('Aeight', 4));
    await openShiaijo(page, 'N');
    const semi = await ensureRunning(page);
    expect(semi, 'a semi-final starts on this court').not.toBeNull();
    await ipponButton(page, winnerOf(semi), 'M').tap();
    await settle(page);
    await context.setOffline(true);
    try {
      await (await armFinish(page)).tap();
      await expect(page.getByText('Offline: 1 result not sent')).toBeVisible();
    } finally {
      await context.setOffline(false);
    }
  });

  // bc-ltap (found by a fresh-clone run, 2026-10-12; operator: "This should be
  // two events"): a mark taken off and tapped back while the removal's save is
  // still out stays on the board and is sent, and so does the reverse. On
  // localhost the removal's echo arrives within milliseconds of the save going
  // out, so the save is held until the test releases it, and it does so only
  // after the tap-back has been dispatched, as a slow uplink would hold it
  // (a safety timeout lets go of a hold nobody released).
  test('bc-ltap: a mark tapped back while its removal is saving stays, and is saved', async ({ page }) => {
    const shot = journeyShots('knockout-mixed-individual/bc-ltap');
    await login(page);
    await seedOwn(page, { name: 'Finding A9', format: 'knockout', courts: ['O'], numberPrefix: 'FI' }, rosterOf('Anine', 4));
    await openShiaijo(page, 'O');
    expect(await ensureRunning(page), 'a semi-final starts on O').not.toBeNull();
    const writes = await holdScoreWrites(page);
    try {
      const akaMark = () => inlineEditor(page).locator('[aria-label^="Aka slot "][aria-label*=": remove "]').first();
      const marksAfterReload = async () => {
        await page.reload();
        await expect.poll(() => inlineRunning(page), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
        return filledSlots(page, 'aka').count();
      };

      await ipponButton(page, 'aka', 'M').tap();
      await settle(page);
      // Remove, then the same mark back while the removal is out.
      let held = writes.holdNext();
      await akaMark().tap();
      await held.sent;
      await ipponButton(page, 'aka', 'M').tap();
      held.release();
      // The removal's response reached the page before the tap-back's own save went out.
      await writes.answeredBeforeNextWrite(held);
      await settle(page);
      // Deliberate: a late echo that undoes the tap-back must be given the time to land, so the time is the evidence.
      await page.waitForTimeout(1000);
      await expect(filledSlots(page, 'aka'), 'the tapped-back mark stays').toHaveCount(1);
      await shot(page, 'tapped-back-mark-kept');
      expect(await marksAfterReload(), 'and the server holds it').toBe(1);
      await shot(page, 'tapped-back-mark-after-reload');

      // The reverse: take it off, add it, and take it off again while the add is out.
      await akaMark().tap();
      await settle(page);
      held = writes.holdNext();
      await ipponButton(page, 'aka', 'M').tap();
      await held.sent;
      await akaMark().tap();
      held.release();
      await writes.answeredBeforeNextWrite(held);
      await settle(page);
      // Deliberate: a late echo that undoes the tap-back must be given the time to land, so the time is the evidence.
      await page.waitForTimeout(1000);
      await expect(filledSlots(page, 'aka'), 'the removal stays').toHaveCount(0);
      expect(await marksAfterReload(), 'and the server holds no mark').toBe(0);
    } finally {
      await writes.stop();
    }
    writes.assertNoSafetyRelease(); // after the body succeeded: a throw in the finally would hide its error
  });
});
