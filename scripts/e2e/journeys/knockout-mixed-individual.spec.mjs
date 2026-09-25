// Operator review journeys for INDIVIDUAL competitions (mp-yqxn.2, reviewer A).
//
// Every fixture is built through the interface in beforeAll (first-run form,
// wizard, roster paste box, Generate draw, Start), one server per worker:
//   F1  pools + knockout, 12 entrants, shiaijo A and B      (J1)
//   F1b pools + knockout, 6 entrants, shiaijo E             (J2 decisions)
//   F2  knockout only, joint 3rd UNTICKED (bronze), shiaijo C (J2 hantei, J6)
//   F2d knockout only, default joint 3rd, shiaijo D         (J8 Bracket tab)
//   F1c pools + knockout, 6 entrants, shiaijo F             (J8 Pools tab)
// Each journey performs the correct path and the clumsy-operator variants
// (fixtures/clumsy.mjs, plus fixtures/individual.mjs), records one AUDIT row
// per action x variant (written to output/knockout-mixed-individual/audit.json),
// and asserts only what the docs promise. Judgement (friction, legibility) is
// formed from the screenshots in output/knockout-mixed-individual/, never
// asserted here. The <bead-id> tests at the end are functional defects
// the journeys found; each seeds its own competition so it runs alone once
// its bead fixes the defect and the fixme is removed.
import { test, expect, isCoarse } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import {
  openShiaijo, upNextCard, inlineEditor, sides, completedRows, lastCompleted,
} from '../fixtures/shiaijo.mjs';
import {
  INLINE_EDITOR, EDITOR, ipponButton, syncState, filledSlots, finishButton, armedFinishButton, armFinish, finishMatch,
} from '../fixtures/scoring.mjs';
import { doubleTap, hastyConfirm, interrupt, retapWhileSaving, tapNeighbour } from '../fixtures/clumsy.mjs';
import {
  ROSTER_12, ROSTER_6, ROSTER_4_BRONZE, ROSTER_4_JOINT, ROSTER_6B, rosterOf, auditLog, tapSize, inlineIdentity, overlayIdentity,
  editorSides, slotMarks, winnerOf, inlineRunning, ensureRunning, playRunningBout, completedRowFor,
  hastyShiaijoConfirm, decisionButton, decisionPrompt, recordDecision, queuedPoolBouts,
} from '../fixtures/individual.mjs';

test.describe.configure({ mode: 'serial' });

const comps = {};
const audit = auditLog();

// Let the running bout's autosave land: the pill reads "Synced" during the
// 300ms debounce as well, so the pill alone does not prove the write went out.
async function settle(page, root = INLINE_EDITOR) {
  await page.waitForTimeout(700);
  await expect(syncState(page, root)).toHaveText('Synced');
}

test.describe('knockout-mixed-individual', () => {
  test.beforeAll(async ({ browser, server }) => {
    test.setTimeout(240_000);
    const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: server.base });
    const page = await ctx.newPage();
    await createTournament(page, { name: 'Individual Review Cup', courts: 15 });
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

  test.afterAll(() => audit.flush());

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
    await expect.poll(() => inlineRunning(page), { timeout: 15_000 }).toBe(true);
    const running1 = await editorSides(page);
    const inProgress = (await page.locator('.admin-live-strip, .live-strip').allInnerTexts()).join(' ');
    await page.waitForTimeout(800);
    const upNextAfter = await sides(upNextCard(page)).catch(() => null);
    row({ step: 'start bout', action: 'Start match (Up next card)', variant: 'V2 doubleTap', ...dbl,
      intended: firstPair, editorHolds: running1, upNextAfter, liveStrip: inProgress,
      shot: await shot(page, 'start-double-tapped') });
    expect(running1).toEqual(firstPair);

    const id1 = await inlineIdentity(page);
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

    const dblIppon = await doubleTap(ipponButton(page, w1, 'M'));
    await expect(syncState(page)).toHaveText('Synced');
    const afterDbl = await slotMarks(page, w1);
    row({ step: 'award ippon', action: `${w1} M`, variant: 'V2 doubleTap', ...dblIppon, marks: afterDbl,
      shot: await shot(page, 'ippon-double-tapped') });

    // V2 on the undo: a double tap on the outer mark.
    const outer = inlineEditor(page).locator(`[aria-label^="${w1 === 'shiro' ? 'Shiro' : 'Aka'} slot "][aria-label*=": remove "]`).first();
    const dblClear = await doubleTap(outer);
    await expect(syncState(page)).toHaveText('Synced');
    const afterDblClear = await slotMarks(page, w1);
    row({ step: 'clear mark', action: 'tap a scored mark', variant: 'V2 doubleTap', ...dblClear,
      marksBefore: afterDbl, marksAfter: afterDblClear, shot: await shot(page, 'mark-double-tapped') });
    // Put the bout back to one men for the winner.
    while ((await filledSlots(page, w1).count()) > 1) await filledSlots(page, w1).first().tap();
    if ((await filledSlots(page, w1).count()) === 0) await ipponButton(page, w1, 'M').tap();
    await expect(filledSlots(page, w1)).toHaveCount(1);
    await settle(page);

    // V3 edge: a reload INSIDE the 300ms autosave debounce, right after a tap.
    // The pill already reads "Synced" at that moment.
    await ipponButton(page, other, 'K').tap();
    const pillAtTap = (await syncState(page).innerText()).trim();
    await interrupt(page, 'reload');
    await expect.poll(() => inlineRunning(page), { timeout: 15_000 }).toBe(true);
    await page.waitForTimeout(1000);
    const otherAfterFastReload = await slotMarks(page, other);
    row({ step: 'running bout', action: `${other} K then reload within 300ms`, variant: 'V3 interrupt reload (inside autosave debounce)',
      pillAtTap, marksAfterReload: otherAfterFastReload, shot: await shot(page, 'fast-reload-after-tap') });
    // Whatever survived, go back to one men for the winner and nothing else.
    for (const sd of [other, w1]) {
      while ((await filledSlots(page, sd).count()) > 0) {
        await filledSlots(page, sd).first().tap();
        await page.waitForTimeout(150);
      }
    }
    await ipponButton(page, w1, 'M').tap();
    await expect(filledSlots(page, w1)).toHaveCount(1);
    await expect(filledSlots(page, other)).toHaveCount(0);
    await settle(page);

    // V3 interruptions mid-bout, and V5: same court, same match, marks kept?
    for (const kind of ['reload', 'hidden', 'back', 'offline']) {
      const r = await interrupt(page, kind);
      const back = await expect.poll(() => inlineRunning(page), { timeout: 15_000 }).toBe(true).then(() => true, () => false);
      const id = back ? await inlineIdentity(page) : null;
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
    await page.waitForTimeout(1500);
    const finishedByDouble = await completedRowFor(page, running1).isVisible().catch(() => false);
    const armedLeft = await armedFinishButton(page).isVisible().catch(() => false);
    row({ step: 'finish', action: 'Finish + Start Next', variant: 'V2 doubleTap', ...dblFinish,
      finishedByDoubleTap: finishedByDouble, stillArmed: armedLeft, shot: await shot(page, 'finish-double-tapped') });
    if (!finishedByDouble) {
      if (!armedLeft) await finishButton(page).tap();
      await armedFinishButton(page).tap();
    }
    const r1 = await lastCompleted(page);
    expect(r1).toMatchObject({ shiro: running1.shiro, aka: running1.aka });
    expect(r1.result).toMatch(/M/);

    // Finish + Start Next started the next bout on THIS court.
    await expect.poll(() => inlineRunning(page), { timeout: 15_000 }).toBe(true);
    const id2 = await inlineIdentity(page);
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
    await page.waitForTimeout(500);
    row({ step: 'finish', action: 'armed Finish', variant: 'V1 tapNeighbour left', hit: nbFinish.hit ? nbFinish.hit.label : null,
      note: nbFinish.note, stillArmed: await armedFinishButton(page).isVisible().catch(() => false),
      editorCourt: (await inlineIdentity(page)).court, shot: await shot(page, 'finish-neighbour-tapped') });
    if (!(await armedFinishButton(page).isVisible().catch(() => false))) await armFinish(page);
    const retap = await retapWhileSaving(armedFinishButton(page));
    await expect(completedRowFor(page, running2)).toBeVisible();
    await expect.poll(() => inlineRunning(page), { timeout: 15_000 }).toBe(true);
    const running3 = await editorSides(page);
    const completedCount = await completedRows(page).count();
    row({ step: 'finish', action: 'armed Finish', variant: 'V2 retapWhileSaving', ...retap,
      completedRows: completedCount, nextRunning: running3, court: (await inlineIdentity(page)).court,
      shot: await shot(page, 'finish-retapped-while-saving') });
    expect((await inlineIdentity(page)).court).toBe('A');

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
    await page.waitForTimeout(800);
    const idleAfter = await page.locator('.shiaijo__placeholder').isVisible().catch(() => false);
    const upNextAfterRevert = await sides(upNextCard(page)).catch(() => null);
    row({ step: 'send back', action: 'Send back to queue confirm', variant: 'V4 hastyConfirm', ...hasty,
      marksOnBoard: marksBeforeSendBack, dialogShot: sendBackDialogShot, courtIdle: idleAfter, upNext: upNextAfterRevert,
      shot: await shot(page, 'send-back-hasty') });
    // It is back in the queue; start it again and play it.
    const again = await ensureRunning(page);
    row({ step: 'send back', action: 'restart the sent-back bout', variant: 'recovery',
      restarted: again, sameAsSentBack: !!again && again.shiro === running3.shiro && again.aka === running3.aka,
      marksAfterRestart: again ? await slotMarks(page, winnerOf(again)) : null });

    // Play out court A's pool bouts but the last one.
    for (let i = 0; i < 8 && (await queuedPoolBouts(page)) > 0; i += 1) await playRunningBout(page);
    await expect.poll(() => inlineRunning(page), { timeout: 15_000 }).toBe(true);
    expect((await inlineIdentity(page)).eyebrow).toMatch(/POOL/);
    await shot(page, 'court-A-last-pool-bout-running');

    // Correct a completed bout in place while the last pool bout runs.
    const firstDone = completedRowFor(page, running1);
    const correctBtn = firstDone.getByRole('button', { name: 'Correct' });
    row({ step: 'correct', action: 'Correct (completed row)', variant: 'size', ...(await tapSize(correctBtn)) });
    await correctBtn.tap();
    await expect(inlineEditor(page).locator('.editor-head-pill')).toHaveText('CORRECTION');
    const corrId = await inlineIdentity(page);
    await shot(page, 'correcting-bout-1');
    await ipponButton(page, w1, 'K').tap();
    await inlineEditor(page).getByRole('button', { name: 'Save correction' }).tap();
    const reason = inlineEditor(page).locator('.reason-prompt');
    await expect(reason).toBeVisible();
    await shot(page, 'correction-reason');
    await reason.locator('button.btn--primary').tap();
    await page.waitForTimeout(800);
    const backToCourt = page.getByRole('button', { name: /Back to court/ });
    const stillCorrecting = await backToCourt.isVisible().catch(() => false);
    row({ step: 'correct', action: 'Save correction (reason prompt, loud button)', variant: 'V4 hasty (inline prompt)',
      editorCourt: corrId.court, stayedOnCorrection: stillCorrecting, result: (await completedRowFor(page, running1).locator('.shiaijo-qrow__result').innerText()).trim(),
      shot: await shot(page, 'correction-saved') });
    if (stillCorrecting) await backToCourt.tap();
    await expect.poll(() => inlineRunning(page), { timeout: 15_000 }).toBe(true);
    const lastPoolA = await editorSides(page);
    await shot(page, 'back-to-live-bout');

    // Court B plays out all its pool bouts, so court A's last bout is the one
    // that closes the pool phase.
    await openShiaijo(page, 'B');
    const bOrder = [];
    for (let i = 0; i < 12; i += 1) {
      const running = await inlineRunning(page);
      const eyebrow = running ? (await inlineIdentity(page)).eyebrow : '';
      if ((await queuedPoolBouts(page)) === 0 && !/POOL/.test(eyebrow)) break;
      const played = await playRunningBout(page);
      bOrder.push({ ...played, eyebrow: eyebrow || '(started from Up next)' });
      // Did Finish + Start Next leave the court idle with pool bouts queued?
      await page.waitForTimeout(1500);
      if (!(await inlineRunning(page)) && (await queuedPoolBouts(page)) > 0) {
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
    await expect.poll(() => inlineRunning(page), { timeout: 15_000 }).toBe(true);
    expect(await editorSides(page)).toEqual(lastPoolA);
    const labelAtBoundary = (await finishButton(page).innerText()).trim();
    await ipponButton(page, winnerOf(lastPoolA), 'M').tap();
    await expect(syncState(page)).toHaveText('Synced');
    await finishMatch(page, INLINE_EDITOR);
    await expect(completedRowFor(page, lastPoolA)).toBeVisible();
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
      const idk = await inlineIdentity(page);
      expect(idk.court).toBe('A');
      await playRunningBout(page);
      row({ step: `knockout bout ${i + 1}`, action: 'Finish + Start Next', variant: 'correct path', pair: p, eyebrow: idk.eyebrow,
        court: idk.court });
    }
    await shot(page, 'court-A-knockout-chained');

    // Docs (Score a match): on a laptop, Left / Right move to the previous or
    // next match on the court and Esc closes the editor. Tried on the console.
    const liveK = await ensureRunning(page);
    if (liveK) {
      const before = await inlineIdentity(page);
      await inlineEditor(page).locator('.editor-modal__title').click();
      const seenK = [];
      for (const key of ['ArrowRight', 'ArrowLeft', 'Escape']) {
        await page.keyboard.press(key);
        await page.waitForTimeout(300);
        seenK.push({ key, eyebrow: (await inlineIdentity(page).catch(() => ({ eyebrow: null }))).eyebrow,
          editor: await inlineEditor(page).isVisible().catch(() => false) });
      }
      row({ step: 'console keys', action: 'ArrowRight / ArrowLeft / Esc on the court console', variant: 'correct path (docs: laptop keys)',
        before: before.eyebrow, after: seenK, shot: await shot(page, 'console-keys') });
    }

    // Scores tab overlay: Prev / Next and the arrow keys stay on the court.
    await page.goto(`/admin/competition/${comps.F1}/scores`);
    const rowB = page.locator('.score-edit-row').filter({ has: page.locator('.score-edit-row__court', { hasText: /^B\b/ }) })
      .filter({ has: page.getByRole('button', { name: /^Score$/ }) }).first();
    await rowB.getByRole('button', { name: /^Score$/ }).tap();
    await expect(page.locator(EDITOR)).toBeVisible();
    const seen = [await overlayIdentity(page)];
    await shot(page, 'scores-overlay-B');
    const next = page.locator(EDITOR).locator('.score-nav__next');
    const prev = page.locator(EDITOR).locator('.score-nav__prev');
    row({ step: 'scores tab nav', action: 'Prev / Next', variant: 'size', next: await tapSize(next).catch(() => null), prev: await tapSize(prev).catch(() => null) });
    for (let i = 0; i < 3 && await next.isVisible().catch(() => false); i += 1) {
      await next.tap();
      seen.push(await overlayIdentity(page));
    }
    for (const key of ['ArrowLeft', 'ArrowRight', 'ArrowLeft']) {
      await page.keyboard.press(key);
      await page.waitForTimeout(200);
      seen.push(await overlayIdentity(page));
    }
    row({ step: 'scores tab nav', action: 'Next x3, ArrowLeft/Right', variant: 'correct path', courts: seen.map((s) => s.court), eyebrows: seen.map((s) => s.eyebrow),
      shot: await shot(page, 'scores-overlay-after-nav') });
    for (const s of seen) expect(s.court).toBe('B');
  });

  // J2. Special decisions. On F1b (court E, pools): a kiken recorded against
  // the WRONG side by a hasty operator (the prompt defaults to Shiro), its
  // default-win chain, then the undo through the 409 decision_locked confirm;
  // a fusenpai that auto-advances. On F2 (court C, knockout): a tied 1-1 bout
  // through encho (an unbounded counter) to hantei.
  test('J2 special decisions: kiken chain and undo, fusenpai, encho, hantei', async ({ page }) => {
    test.setTimeout(240_000);
    const J = 'J2';
    const shot = journeyShots('knockout-mixed-individual/J2');
    const row = (r) => audit.record({ journey: J, ...r });
    await login(page);
    await openShiaijo(page, 'E');

    // Pool A's first bout. The competitor who withdraws is AKA.
    const b1 = await ensureRunning(page);
    const id1 = await inlineIdentity(page);
    expect(id1.eyebrow).toMatch(/POOL A/);
    for (const kind of ['kiken-voluntary', 'kiken-injury', 'fusenpai']) {
      row({ step: 'decision controls', action: kind, variant: 'size', ...(await tapSize(decisionButton(page, kind))) });
    }
    row({ step: 'decision controls', action: 'Decide by hantei…', variant: 'size',
      ...(await tapSize(inlineEditor(page).getByTestId('scoring-modal-hantei-arm'))) });

    // V1: the thumb meant Kiken - Voluntary and landed on its neighbour.
    const nbKiken = await tapNeighbour(decisionButton(page, 'kiken-voluntary'), 'right');
    await page.waitForTimeout(400);
    const promptOpen = await decisionPrompt(page).isVisible().catch(() => false);
    const popover = await page.locator('[role="tooltip"], .glossary-pop, .term-pop').first().isVisible().catch(() => false);
    row({ step: 'kiken', action: 'Kiken - Voluntary', variant: 'V1 tapNeighbour right', hit: nbKiken.hit.label, gapPx: nbKiken.gapPx,
      promptOpened: promptOpen, glossaryOpened: popover, committed: false, shot: await shot(page, 'kiken-neighbour-tapped') });
    if (promptOpen) await decisionPrompt(page).getByRole('button', { name: 'Cancel' }).tap();
    await page.keyboard.press('Escape').catch(() => {});

    // V4: the prompt is answered by its loud button without choosing a side.
    await decisionButton(page, 'kiken-voluntary').tap();
    await expect(decisionPrompt(page)).toBeVisible();
    await shot(page, 'kiken-prompt');
    const defaultSide = await decisionPrompt(page).locator('input[name="decision-side"]:checked').getAttribute('value');
    const radio = decisionPrompt(page).locator('input[name="decision-side"]').first();
    row({ step: 'kiken', action: 'side radio (Which side withdrew?)', variant: 'size', ...(await tapSize(radio)),
      label: await tapSize(decisionPrompt(page).locator('label').first()) });
    await decisionPrompt(page).getByRole('button', { name: 'Record' }).tap();
    const remaining = inlineEditor(page).locator('.remaining-matches');
    await expect(remaining).toBeVisible();
    const withdrawnShown = (await remaining.locator('div').first().innerText()).trim();
    const withdrawnName = withdrawnShown.replace(/^Remaining matches for\s*/, '').replace(/\s*✕$/, '').trim();
    row({ step: 'kiken', action: 'Record (prompt left on its default side)', variant: 'V4 hasty (inline prompt)',
      intendedWithdrawer: `aka: ${b1.aka}`, defaultSide, recordedWithdrawer: withdrawnName,
      wrongSide: defaultSide !== 'aka', shot: await shot(page, 'kiken-recorded-default-side') });
    const kikenRow = completedRowFor(page, b1);
    await expect(kikenRow).toBeVisible();
    const kikenResult = (await kikenRow.locator('.shiaijo-qrow__result').innerText()).trim();
    // Docs: the winner shows two circles, Kiken sits beside the withdrawer.
    expect(kikenResult).toMatch(/○/);
    expect(kikenResult).toMatch(/Kiken/);

    // The default-win chain. The operator looks up at the shiaijo for a
    // moment before tapping anything in the remaining-matches panel.
    const panelAtOnce = await remaining.isVisible().catch(() => false);
    await page.waitForTimeout(3000);
    const panelAfter3s = await remaining.isVisible().catch(() => false);
    row({ step: 'kiken chain', action: 'remaining-matches panel, 3s later', variant: 'V3 interrupt (looked away)',
      panelAtOnce, panelAfter3s, editor: await inlineEditor(page).isVisible().catch(() => false),
      idle: await page.locator('.shiaijo__placeholder').isVisible().catch(() => false),
      shot: await shot(page, 'kiken-chain-after-3s') });

    // The withdrawn competitor's next bout is still in this court's queue,
    // now carrying the barred-match notice (bc-tmfn): Start match is gone
    // from that row, replaced by the notice's own one-tap resolution.
    const nextOfWithdrawn = page.locator('.shiaijo-upnext__card, .shiaijo__queue .shiaijo-qrow:not(.shiaijo-qrow--complete)')
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
    expect(noticeText).toBe(`${withdrawnName} withdrew: record the default win.`);

    // Record the default win the way the operator now does: tap the notice's
    // own button, then double-tap it (the impatient thumb) -- it locks itself
    // on the first response, so only one decision must land.
    const defaultWinBtn = notice.getByTestId('barred-match-default-win');
    row({ step: 'kiken chain', action: 'Record default win for the opponent (barred-match notice)', variant: 'size', ...(await tapSize(defaultWinBtn)) });
    const dblDefaultWin = await doubleTap(defaultWinBtn);
    const chainRow = completedRowFor(page, pairW);
    await expect(chainRow).toBeVisible({ timeout: 8000 });
    const chainResult = (await chainRow.locator('.shiaijo-qrow__result').innerText()).trim();
    row({ step: 'kiken chain', action: 'Record default win for the opponent (barred-match notice)', variant: 'V2 doubleTap', ...dblDefaultWin,
      result: chainResult, cardLeftQueue: !(await nextOfWithdrawn.isVisible().catch(() => false)),
      shot: await shot(page, 'chain-default-win-recorded') });
    // The notice records a fusensho: the winner shows two circles and the
    // Fus. mark sits beside them, the side that was present (sideMarks).
    expect(chainResult).toMatch(/○/);
    expect(chainResult).toMatch(/Fus\./);

    // The undo, performed the way it happens: the court has already moved on
    // (Up next started), then the operator realises Aka withdrew, not Shiro.
    await openShiaijo(page, 'E');
    const moved = await ensureRunning(page);
    row({ step: 'kiken undo', action: 'court moves on: Up next started', variant: 'correct path', running: moved,
      involvesRealWithdrawer: !!moved && (moved.shiro === b1.aka || moved.aka === b1.aka) });
    await completedRowFor(page, b1).getByRole('button', { name: 'Correct' }).tap();
    await expect(inlineEditor(page).locator('.editor-head-pill')).toHaveText('CORRECTION');
    await shot(page, 'kiken-correction-open');
    await recordDecision(page, 'kiken-voluntary', { side: 'aka', reason: 'wrong side recorded' });
    const lockedDialog = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();
    const locked = await lockedDialog.waitFor({ state: 'visible', timeout: 8000 }).then(() => true, () => false);
    let hasty = null;
    if (locked) {
      await shot(page, 'decision-locked-confirm');
      hasty = await hastyConfirm(page);
    }
    await page.waitForTimeout(1500);
    const undoPanel = await remaining.isVisible().catch(() => false);
    const undoPanelText = undoPanel ? (await remaining.innerText()).replace(/\s+/g, ' ').slice(0, 240) : null;
    const undoneResult = (await completedRowFor(page, b1).locator('.shiaijo-qrow__result').innerText()).trim();
    row({ step: 'kiken undo', action: 'Kiken - Voluntary on the other side (correction)', variant: 'V4 hastyConfirm (409 decision_locked)',
      dialogShown: locked, ...(hasty || {}), result: undoneResult, remainingPanel: undoPanelText,
      shot: await shot(page, 'kiken-undo-forced') });
    expect(locked).toBe(true);
    // Docs: the kiken now sits beside Aka, the real withdrawer.
    expect(undoneResult).toMatch(/○○ vs Kiken|○○.*Kiken$/);
    let award = null;
    if (undoPanel) {
      const btn = remaining.getByRole('button', { name: 'Award default win to opponent' });
      if (await btn.count()) {
        await btn.first().tap();
        await page.waitForTimeout(1500);
        award = { error: ((await remaining.locator('div[style*="danger"]').allInnerTexts().catch(() => [])) || []).join(' | '),
          left: await btn.count() };
      }
      row({ step: 'kiken undo', action: 'Award default win to opponent (panel after the undo)', variant: 'correct path (the chain)',
        panel: undoPanelText, award, shot: await shot(page, 'undo-chain-award') });
      await remaining.getByRole('button', { name: '✕' }).tap().catch(() => {});
    }
    const backToCourt = page.getByRole('button', { name: /Back to court/ });
    if (await backToCourt.isVisible().catch(() => false)) await backToCourt.tap();
    await page.waitForTimeout(1000);
    const liveAfter = await inlineRunning(page) ? await editorSides(page) : null;
    row({ step: 'kiken undo', action: 'back to the court after the forced undo', variant: 'V5 lost place',
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
      await ipponButton(page, opp, 'M').tap();
      await settle(page);
      await finishButton(page).tap();
      await armedFinishButton(page).tap();
      await page.waitForTimeout(1500);
      const msg = (await page.locator('.toast, .pending-write-banner').allInnerTexts().catch(() => [])).join(' | ');
      const done = await completedRowFor(page, live).isVisible().catch(() => false);
      row({ step: 'kiken undo', action: 'Finish the running bout of the now-withdrawn competitor (M for the opponent)', variant: 'recovery attempt',
        pair: live, completed: done, message: msg.replace(/\s+/g, ' ').slice(0, 240), shot: await shot(page, 'finish-bout-of-withdrawn') });
      if (!done && await inlineRunning(page)) {
        await page.getByRole('button', { name: 'Send back to queue' }).tap();
        await hastyShiaijoConfirm(page);
      }
    }
    const poolBCard = page.locator('.shiaijo-upnext__card, .shiaijo__queue .shiaijo-qrow:not(.shiaijo-qrow--complete)')
      .filter({ hasText: /Match 1 of 3/ }).filter({ hasNotText: withdrawnName }).last();
    const b2 = await sides(poolBCard);
    await poolBCard.getByRole('button', { name: 'Start match' }).tap();
    await expect.poll(() => inlineRunning(page), { timeout: 15_000 }).toBe(true);
    expect(await editorSides(page)).toEqual(b2);
    const id2 = await inlineIdentity(page);
    await decisionButton(page, 'fusenpai').tap();
    await expect(decisionPrompt(page)).toBeVisible();
    await decisionPrompt(page).locator('input[name="decision-side"][value="aka"]').check();
    const dblRecord = await doubleTap(decisionPrompt(page).getByRole('button', { name: 'Record' }));
    await page.waitForTimeout(2000);
    const dialogAfterDbl = await page.locator('.modal[role="dialog"]').isVisible().catch(() => false);
    let dblDialog = null;
    if (dialogAfterDbl) {
      await shot(page, 'fusenpai-double-record-dialog');
      dblDialog = await hastyConfirm(page);
    }
    const fusRow = completedRowFor(page, b2);
    await expect(fusRow).toBeVisible();
    const fusResult = (await fusRow.locator('.shiaijo-qrow__result').innerText()).trim();
    const advanced = await expect.poll(() => inlineRunning(page), { timeout: 8000 }).toBe(true).then(() => true, () => false);
    const id3 = advanced ? await inlineIdentity(page) : null;
    row({ step: 'fusenpai', action: 'Record (fusenpai, Aka did not show)', variant: 'V2 doubleTap', ...dblRecord,
      from: id2.eyebrow, result: fusResult, dialogAfterDoubleTap: dialogAfterDbl, dialog: dblDialog,
      advancedTo: id3 && id3.eyebrow, court: id3 && id3.court, shot: await shot(page, 'fusenpai-recorded') });
    // Docs: the winner shows two circles and Fus. sits beside the no-show.
    expect(fusResult).toMatch(/○/);
    expect(fusResult).toMatch(/Fus\./);
    if (advanced) expect(id3.court).toBe('E');

    // Knockout (F2, court C): a tied 1-1 semi-final, encho, hantei.
    await openShiaijo(page, 'C');
    const semi = await ensureRunning(page);
    expect((await inlineIdentity(page)).eyebrow).toMatch(/SEMIFINAL/);
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
    expect(koTieLabel).toBe('Needs a winner');
    expect(koTieTitle).toBe('Needs a winner: fight encho, then record hantei if still tied.');
    expect(tieCompleted).toBe(false);

    const pill = inlineEditor(page).getByTestId('scoring-modal-encho-pill');
    row({ step: 'encho', action: 'Overtime pill', variant: 'size', ...(await tapSize(pill)) });
    // Docs: "open the Overtime control and tick Encho started". The pill is
    // tapped where a thumb lands, its centre.
    await pill.tap();
    await page.waitForTimeout(500);
    const encho = inlineEditor(page).getByTestId('scoring-modal-encho-checkbox');
    const openedByCentre = await encho.isVisible().catch(() => false);
    const enchoPopover = await page.getByText('Overtime, when a knockout match').isVisible().catch(() => false);
    row({ step: 'encho', action: 'tap the Overtime pill (centre)', variant: 'correct path', counterOpened: openedByCentre,
      glossaryPopoverOpened: enchoPopover, shot: await shot(page, 'overtime-pill-tapped') });
    if (!openedByCentre) {
      await page.mouse.click(5, 5);
      await inlineEditor(page).locator('.encho-pill__icon').tap();
      await expect(encho).toBeVisible();
      row({ step: 'encho', action: 'tap the Overtime pill icon instead', variant: 'recovery', taps: 2, counterOpened: true });
    }
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

    // Hantei: arm, then the thumb lands on the neighbour of SHIRO wins.
    await inlineEditor(page).getByTestId('scoring-modal-hantei-arm').tap();
    const shiroWins = inlineEditor(page).getByTestId('scoring-modal-hantei-shiro');
    const akaWins = inlineEditor(page).getByTestId('scoring-modal-hantei-aka');
    row({ step: 'hantei', action: 'SHIRO wins / AKA wins', variant: 'size', shiro: await tapSize(shiroWins), aka: await tapSize(akaWins) });
    await shot(page, 'hantei-armed');
    const nbHantei = await tapNeighbour(shiroWins, 'right');
    const semiRow = completedRowFor(page, semi);
    const committed = await semiRow.waitFor({ state: 'visible', timeout: 6000 }).then(() => true, () => false);
    const firstRead = committed ? (await semiRow.locator('.shiaijo-qrow__result').innerText()).trim() : null;
    const updatedInPlace = await expect.poll(async () => (await semiRow.locator('.shiaijo-qrow__result').innerText()).trim(), { timeout: 6000 })
      .toMatch(/Ht/).then(() => true, () => false);
    await page.reload();
    await expect(semiRow).toBeVisible();
    const wrongResult = (await semiRow.locator('.shiaijo-qrow__result').innerText()).trim();
    row({ step: 'hantei', action: 'the recorded hantei result, read before and after a reload', variant: 'correct path',
      firstRead, htShownWithin6sWithoutReload: updatedInPlace, afterReload: wrongResult, shot: await shot(page, 'hantei-result-after-reload') });
    row({ step: 'hantei', action: 'SHIRO wins (intended)', variant: 'V1 tapNeighbour right', hit: nbHantei.hit.label, gapPx: nbHantei.gapPx,
      committedImmediately: committed, result: wrongResult, shot: await shot(page, 'hantei-neighbour-tapped') });
    // The recorded result reads as the docs describe: the winner's points,
    // Ht beside the winner, (E) in the centre.
    expect(wrongResult).toMatch(/Ht/);
    expect(wrongResult).toMatch(/\(E\)/);

    // Recovery: Correct the semi-final and give hantei to Shiro.
    await semiRow.getByRole('button', { name: 'Correct' }).tap();
    await expect(inlineEditor(page).locator('.editor-head-pill')).toHaveText('CORRECTION');
    await shot(page, 'hantei-correction-open');
    const armAgain = inlineEditor(page).getByTestId('scoring-modal-hantei-arm');
    if (await armAgain.isVisible().catch(() => false)) await armAgain.tap();
    await inlineEditor(page).getByTestId('scoring-modal-hantei-shiro').tap();
    await page.waitForTimeout(1500);
    const reasonAsked = await inlineEditor(page).locator('.reason-prompt').isVisible().catch(() => false);
    const toast = (await page.locator('.toast').allInnerTexts().catch(() => [])).join(' | ');
    const fixed = (await completedRowFor(page, semi).locator('.shiaijo-qrow__result').innerText()).trim();
    row({ step: 'hantei', action: 'Correct -> SHIRO wins', variant: 'recovery', reasonAsked, toast, result: fixed,
      shot: await shot(page, 'hantei-corrected') });
  });
  // J6. Confirm + advance OFFLINE (F2, court C): the second semi-final is
  // finished with the network down. The write is queued, the local bracket
  // advances; back online, the server must agree.
  test('J6 finish a knockout bout offline, then reconnect', async ({ page, context }) => {
    test.setTimeout(180_000);
    const J = 'J6';
    const shot = journeyShots('knockout-mixed-individual/J6');
    const row = (r) => audit.record({ journey: J, ...r });
    await login(page);
    await openShiaijo(page, 'C');
    // Semi-final 1 (M1) online first unless J2 already played it.
    await expect.poll(async () => (await inlineRunning(page)) || (await upNextCard(page).isVisible().catch(() => false)), { timeout: 15_000 }).toBe(true);
    let played = null;
    const liveNow = (await inlineRunning(page)) ? (await inlineIdentity(page)).eyebrow : '';
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
    }, { timeout: 15_000 }).not.toBe('waiting');
    if (!semi) {
      semi = await sides(upNextCard(page));
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      await expect.poll(async () => ((await inlineRunning(page)) ? JSON.stringify(await editorSides(page)) : ''), { timeout: 15_000 })
        .toBe(JSON.stringify(semi));
    }
    row({ step: 'finish offline', action: 'the last semi-final is running', variant: 'correct path', semi });
    expect((await inlineIdentity(page)).eyebrow).toMatch(/SEMIFINAL/);
    const w = winnerOf(semi);
    const winnerName = semi[w];
    const loserName = semi[w === 'shiro' ? 'aka' : 'shiro'];
    await ipponButton(page, w, 'M').tap();
    await settle(page);

    // Network down; Finish (two taps).
    await context.setOffline(true);
    await finishButton(page).tap();
    await armedFinishButton(page).tap();
    await page.waitForTimeout(300);
    const at300 = {
      editor: await inlineEditor(page).isVisible().catch(() => false),
      banner: (await page.locator('.pending-write-banner').allInnerTexts()).join(' | '),
      upNext: await sides(upNextCard(page)).catch(() => null),
      status: ((await page.locator('.admin-conn, [aria-label*="real-time"], [role="status"]').allInnerTexts().catch(() => [])) || []).join(' | ').slice(0, 120),
    };
    row({ step: 'finish offline', action: 'the screen 300ms after Finish, offline', variant: 'V5 lost place', ...at300,
      shot: await shot(page, 'offline-finish-300ms') });
    await page.waitForTimeout(1700);
    const banner = inlineEditor(page).locator('.pending-write-banner');
    const bannerText = (await banner.allInnerTexts()).join(' | ');
    const editorStill = await inlineEditor(page).isVisible().catch(() => false);
    const editorId = editorStill ? await inlineIdentity(page) : null;
    const pill = editorStill ? ((await syncState(page).allInnerTexts())[0] || '').trim() : null;
    const finalRow = page.locator('.shiaijo__queue').getByText(/Veterans Knockout · Match 3\b/).locator('xpath=ancestor::*[contains(@class,"shiaijo-qrow") or contains(@class,"shiaijo-upnext__card")][1]');
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

    // A second device (fresh page) reads the server's bracket.
    const other = await context.newPage();
    await other.goto(`/admin/competition/${comps.F2}/bracket`);
    await expect(other.getByText('3RD PLACE MATCH')).toBeVisible();
    const bracketText = (await other.locator('.page').first().innerText()).replace(/\s+/g, ' ');
    row({ step: 'reconnect', action: 'second page reads the bracket', variant: 'correct path',
      finalHasWinner: bracketText.includes(winnerName), bronzeHasLoser: (bracketText.split('3RD PLACE MATCH')[1] || '').includes(loserName),
      shot: await shot(other, 'server-bracket-after-reconnect', { fullPage: true }) });
    expect((bracketText.split('3RD PLACE MATCH')[1] || '')).toContain(loserName);
    await other.close();
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
    const idP = await overlayIdentity(page);
    await shot(page, 'pools-tab-editor-open');
    const start = page.locator(EDITOR).getByRole('button', { name: 'Start match' });
    const startOffered = await start.isVisible().catch(() => false);
    if (startOffered) await start.tap();
    await page.waitForTimeout(1500);
    const openAfterStart = await page.locator(EDITOR).isVisible().catch(() => false);
    row({ step: 'pools tab', action: 'Start match in the Pools-tab editor', variant: 'correct path', row: rowText.slice(0, 80),
      startOffered, editorStillOpen: openAfterStart, shot: await shot(page, 'pools-tab-after-start') });
    if (!openAfterStart) {
      await firstRow.tap();
      await expect(page.locator(EDITOR)).toBeVisible();
    }
    const w = winnerOf(pair);
    await ipponButton(page, w, 'M', EDITOR).tap();
    await page.waitForTimeout(1500);
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
    await page.waitForTimeout(800);
    const after = {
      editorOpen: await page.locator(EDITOR).isVisible().catch(() => false),
      url: new URL(page.url()).pathname,
      rowNow: (await firstRow.innerText()).replace(/\s+/g, ' ').slice(0, 120),
      toast: (await page.locator('.toast').allInnerTexts().catch(() => [])).join(' | '),
    };
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
      await page.locator(EDITOR).getByRole('button', { name: 'Start match' }).tap();
      await page.waitForTimeout(1000);
      if (!(await page.locator(EDITOR).isVisible().catch(() => false))) await secondRow.tap();
    }
    await ipponButton(page, winnerOf(pair2), 'M', EDITOR).tap();
    // Close straight away (inside the autosave's 300ms), as a hasty operator would.
    const closedByTap = await page.locator(EDITOR).getByRole('button', { name: /✕ Close/ }).tap({ timeout: 2000 }).then(() => true, () => false);
    const discardAsked = await page.locator('.modal[role="dialog"] .modal__foot').isVisible().catch(() => false);
    const hasty = discardAsked ? await hastyConfirm(page) : null;
    await page.waitForTimeout(800);
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
    await page.waitForTimeout(1000);
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
    await page.waitForTimeout(500);
    row({ step: 'bracket tab', action: 'Edit result confirm', variant: 'V4 hastyConfirm', ...editDialog,
      editorReopened: await panel.locator(INLINE_EDITOR).isVisible().catch(() => false),
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
    await finishMatch(page, INLINE_EDITOR);
    await page.waitForTimeout(1000);
    const bracketText = (await page.locator('.page').first().innerText()).replace(/\s+/g, ' ');
    row({ step: 'bracket tab', action: 'second semi-final, default joint 3rd places', variant: 'correct path', pair: bpair2,
      bronzeMatchShown: /3RD PLACE MATCH/i.test(bracketText), shot: await shot(page, 'bracket-tab-semis-done', { fullPage: true }) });
    expect(bracketText).not.toMatch(/3RD PLACE MATCH/i);

    // Fusenpai auto-advance on a court where nothing is stuck (court F, F1c):
    // the J2 attempt ran into a withdrawn competitor's unstartable bout.
    await openShiaijo(page, 'F');
    const fb = await ensureRunning(page);
    if (fb) {
      await recordDecision(page, 'fusenpai', { side: 'aka' });
      await expect(completedRowFor(page, fb)).toBeVisible();
      const advanced = await expect.poll(async () => {
        if (!(await inlineRunning(page))) return 'idle';
        const now = await editorSides(page);
        return now.shiro === fb.shiro && now.aka === fb.aka ? 'same' : 'moved';
      }, { timeout: 10_000 }).toBe('moved').then(() => true, () => false);
      row({ step: 'fusenpai (court F)', action: 'Record fusenpai on the console', variant: 'correct path (auto-advance)',
        from: fb, advanced, court: advanced ? (await inlineIdentity(page)).court : null,
        shot: await shot(page, 'fusenpai-auto-advance-F') });
      if (advanced) expect((await inlineIdentity(page)).court).toBe('F');
    }
  });
  // Functional defects found by the journeys above. Each seeds its own
  // competition on its own shiaijo so it can run alone once fixed.
  const seedOwn = async (page, opts, roster) => {
    const id = await createCompetition(page, { kind: 'individual', ...opts });
    await pasteRoster(page, id, roster);
    await generateDraw(page, id);
    await startCompetition(page, id);
    return id;
  };

  test.fixme('bc-kpnl: after a kiken on the shiaijo console the remaining-matches panel stays until the operator closes it', async ({ page }) => {
    await login(page);
    await seedOwn(page, { name: 'Finding A1', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['G'], numberPrefix: 'FA' }, rosterOf('Aone', 6));
    await openShiaijo(page, 'G');
    await ensureRunning(page);
    await recordDecision(page, 'kiken-voluntary', { side: 'aka' });
    const panel = inlineEditor(page).locator('.remaining-matches');
    await expect(panel).toBeVisible();
    await page.waitForTimeout(3000);
    await expect(panel).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Award default win to opponent' })).toHaveCount(1);
  });

  test.fixme('bc-kfup: a withdrawn competitor\'s remaining pool bout can be recorded as their default loss', async ({ page }) => {
    await login(page);
    const id = await seedOwn(page, { name: 'Finding A2', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['H'], numberPrefix: 'FB' }, rosterOf('Atwo', 6));
    await openShiaijo(page, 'H');
    const first = await ensureRunning(page);
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
    await expect(page.locator(EDITOR).getByText('already_ineligible')).toHaveCount(0);
    await expect(page.locator('.score-edit-row').filter({ hasText: first.aka }).filter({ hasText: /Fus\./ })).toHaveCount(1);
  });

  test.fixme('bc-otpl: tapping the Overtime pill opens the encho counter, as the docs describe', async ({ page }) => {
    await login(page);
    await seedOwn(page, { name: 'Finding A3', format: 'knockout', courts: ['I'], numberPrefix: 'FC' }, rosterOf('Athree', 4));
    await openShiaijo(page, 'I');
    await ensureRunning(page);
    await inlineEditor(page).getByTestId('scoring-modal-encho-pill').tap();
    await expect(inlineEditor(page).getByTestId('scoring-modal-encho-checkbox')).toBeVisible();
  });

  test.fixme('bc-htcr: a hantei verdict can be corrected to the other side from the Correct editor', async ({ page }) => {
    await login(page);
    await seedOwn(page, { name: 'Finding A4', format: 'knockout', courts: ['J'], numberPrefix: 'FD' }, rosterOf('Afour', 4));
    await openShiaijo(page, 'J');
    const semi = await ensureRunning(page);
    await ipponButton(page, 'shiro', 'M').tap();
    await ipponButton(page, 'aka', 'K').tap();
    await settle(page);
    await inlineEditor(page).getByTestId('scoring-modal-hantei-arm').tap();
    await inlineEditor(page).getByTestId('scoring-modal-hantei-aka').tap();
    const done = completedRowFor(page, semi);
    await expect(done).toBeVisible();
    await expect(done.locator('.shiaijo-qrow__result')).toContainText('Ht');
    const before = (await done.locator('.shiaijo-qrow__result').innerText()).trim();
    await done.getByRole('button', { name: 'Correct' }).tap();
    await expect(inlineEditor(page).locator('.editor-head-pill')).toHaveText('CORRECTION');
    const arm = inlineEditor(page).getByTestId('scoring-modal-hantei-arm');
    if (await arm.isVisible().catch(() => false)) await arm.tap();
    await inlineEditor(page).getByTestId('scoring-modal-hantei-shiro').tap();
    // Docs: a correction asks for a short reason, then applies.
    const reason = inlineEditor(page).locator('.reason-prompt');
    if (await reason.isVisible().catch(() => false)) await reason.locator('button.btn--primary').tap();
    await page.waitForTimeout(2000);
    await expect(page.getByText(/correctionReason/)).toHaveCount(0);
    await expect.poll(async () => (await done.locator('.shiaijo-qrow__result').innerText()).trim(), { timeout: 10_000 }).not.toBe(before);
  });

  test.fixme('bc-plcl: the Pools-tab score editor stays open through Start match and an ippon', async ({ page }) => {
    await login(page);
    const id = await seedOwn(page, { name: 'Finding A5', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['K'], numberPrefix: 'FE' }, rosterOf('Afive', 6));
    await page.goto(`/admin/competition/${id}/pools`);
    const first = page.locator('.pool-match-numbered-list > *').first();
    await first.tap();
    await expect(page.locator(EDITOR)).toBeVisible();
    const pair = await editorSides(page, EDITOR);
    await page.locator(EDITOR).getByRole('button', { name: 'Start match' }).tap();
    await page.waitForTimeout(1500);
    await expect(page.locator(EDITOR)).toBeVisible();
    await ipponButton(page, winnerOf(pair), 'M', EDITOR).tap();
    await page.waitForTimeout(1500);
    await expect(page.locator(EDITOR)).toBeVisible();
  });
  test.fixme('bc-sbq: Send back to queue warns that the score will be discarded whenever a mark is on the board', async ({ page }) => {
    await login(page);
    await seedOwn(page, { name: 'Finding A6', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['L'], numberPrefix: 'FF' }, rosterOf('Asix', 6));
    await openShiaijo(page, 'L');
    const pair = await ensureRunning(page);
    // Venue wifi: the court's refresh after the autosave is slow to arrive.
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
    await expect(dialog).toContainText('will be discarded');
    await expect(dialog).not.toContainText('nothing will be lost');
    await page.unroute('**/api/**', slowReads);
  });
  test.fixme('bc-kosc: in pools + knockout, no knockout bout is scheduled before the last pool bout', async ({ page }) => {
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
  test.fixme('bc-offl: finishing a bout offline on the court console says the result is not saved yet', async ({ page, context }) => {
    await login(page);
    await seedOwn(page, { name: 'Finding A8', format: 'knockout', courts: ['N'], numberPrefix: 'FH' }, rosterOf('Aeight', 4));
    await openShiaijo(page, 'N');
    const semi = await ensureRunning(page);
    await ipponButton(page, winnerOf(semi), 'M').tap();
    await settle(page);
    await context.setOffline(true);
    try {
      await finishButton(page).tap();
      await armedFinishButton(page).tap();
      await page.waitForTimeout(1000);
      await expect(page.getByText(/Not saved yet/)).toBeVisible();
    } finally {
      await context.setOffline(false);
    }
  });
});
