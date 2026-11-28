// Smoke journey: proves the harness end to end, and every clumsy helper once.
//
// The operator creates the tournament, signs in through the form, builds an
// individual pools + knockout competition through the wizard, pastes a roster,
// draws, starts, and scores on /admin/shiaijo/A and B; a bout of B is then
// corrected from the Scores tab in the overlay editor, and a spectator's phone
// sees the result. Everything is seeded through the interface.
//
// The clumsy variants are PERFORMED and RECORDED here (printed as AUDIT lines
// and written to the journey folder's audit.json, fixtures/audit.mjs), never
// asserted: this spec only asserts what the seeding path and the one
// cross-surface result need. The knockout-mixed journeys are where variants
// get judged.
import { test, expect, isCoarse } from '../fixtures/test.mjs';
import { PUBLIC_DEVICE, EXPECT_TIMEOUT_MS } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { createTournament, login, signOut } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, requestDiscardDraw, startCompetition } from '../fixtures/competition.mjs';
import {
  editorIdentity, inlineEditor, lastCompleted, openShiaijo, sides, startUpNext, upNextCard,
} from '../fixtures/shiaijo.mjs';
import {
  EDITOR, INLINE_EDITOR, armFinish, awardIppon, filledSlots, finishMatch, ipponButton, nextMatchWrite, settled, unsentCount, writeLanded,
} from '../fixtures/scoring.mjs';
import { scoreRows } from '../fixtures/scores.mjs';
import { openTvBoard, openViewer, recentResult, tvBoard } from '../fixtures/public.mjs';
import { doubleTap, hastyConfirm, interrupt, retapWhileSaving, tapNeighbour } from '../fixtures/clumsy.mjs';

// Eight entrants, four dojos, (name, dojo) unique.
const ROSTER = [
  ['Akira Tanaka', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Chie Mori', 'Musashi'],
  ['Dan Evans', 'Seishinkan'], ['Eri Kato', 'Gyokusen'], ['Finn Hale', 'Thames'],
  ['Goro Abe', 'Musashi'], ['Hana Ito', 'Seishinkan'],
];

test.describe.configure({ mode: 'serial' });

test.describe('smoke', () => {
  let shot;
  const audit = auditLog('smoke');
  const { record } = audit;
  test.beforeAll(() => {
    shot = journeyShots('smoke');
  });

  test.afterAll(() => audit.flush());

  test('seed through the UI, score on a shiaijo, see it on a public phone', async ({ page, browser, baseURL }) => {
    let compId;
    let bout1;

    await test.step('create the tournament, sign out, sign in through the form', async () => {
      await createTournament(page, { name: 'E2E Cup', courts: 2 });
      await signOut(page);
      await login(page);
      await shot(page, 'signed-in');
    });

    await test.step('create an individual pools + knockout competition in the wizard', async () => {
      compId = await createCompetition(page, {
        name: 'Men Individual', kind: 'individual', format: 'mixed', courts: ['A', 'B'], numberPrefix: 'E',
      });
      await shot(page, 'competition-created');
    });

    await test.step('paste the roster', async () => {
      await pasteRoster(page, compId, ROSTER);
      await shot(page, 'roster-applied');
    });

    await test.step('generate the draw, answer Discard draw hastily, generate again', async () => {
      await generateDraw(page, compId);
      await shot(page, 'draw-ready');
      await requestDiscardDraw(page);
      await shot(page, 'discard-draw-confirm');
      const answered = await hastyConfirm(page);
      const drawDiscarded = await page.getByRole('button', { name: 'Generate draw' })
        .waitFor({ state: 'visible', timeout: 10_000 }).then(() => true, () => false);
      record({ variant: 'V4 hastyConfirm', action: 'Discard draw', ...answered, drawDiscarded });
      await generateDraw(page, compId);
    });

    await test.step('start the competition', async () => {
      await startCompetition(page, compId);
      await shot(page, 'started-scores-tab');
    });

    await test.step('open shiaijo A on a coarse pointer', async () => {
      await openShiaijo(page, 'A');
      expect(await isCoarse(page)).toBe(true);
      const call = upNextCard(page).getByRole('button', { name: /Call to court|Call again|Calling…/ });
      const tapped = await doubleTap(call);
      const labelAfter = (await call.innerText()).trim();
      record({ variant: 'V2 doubleTap', action: 'Call to court', ...tapped, labelAfter });
      await shot(page, 'shiaijo-A');
    });

    await test.step('bout 1: start from Up next, interrupted three ways, one ippon, Finish', async () => {
      const pair = await startUpNext(page);
      const identity = await editorIdentity(page);

      const reload = await interrupt(page, 'reload');
      const backAfterReload = await inlineEditor(page).waitFor({ state: 'visible', timeout: EXPECT_TIMEOUT_MS })
        .then(() => true, () => false);
      record({ variant: 'V3 interrupt', action: 'running bout', ...reload, editorBack: backAfterReload,
        sameMatch: backAfterReload && (await editorIdentity(page)).key === identity.key });

      await awardIppon(page, 'shiro', 'M');

      const hidden = await interrupt(page, 'hidden');
      record({ variant: 'V3 interrupt', action: 'bout with one ippon', ...hidden,
        sameMatch: (await editorIdentity(page)).key === identity.key });

      const back = await interrupt(page, 'back');
      const backAfterBack = await inlineEditor(page).waitFor({ state: 'visible', timeout: EXPECT_TIMEOUT_MS })
        .then(() => true, () => false);
      const shiroPoints = backAfterBack
        ? await inlineEditor(page).locator('[aria-label^="Shiro slot "][aria-label*=": remove "]').count() : null;
      record({ variant: 'V3 interrupt', action: 'bout with one ippon', ...back, editorBack: backAfterBack,
        sameMatch: backAfterBack && (await editorIdentity(page)).key === identity.key, shiroPointsKept: shiroPoints });
      await shot(page, 'bout-1-scored');

      await finishMatch(page, INLINE_EDITOR);
      bout1 = await lastCompleted(page);
      expect(bout1).toMatchObject({ shiro: pair.shiro, aka: pair.aka });
      expect(bout1.result).toMatch(/^M\b/);
      await shot(page, 'bout-1-finished');
    });

    await test.step('bout 2: a neighbour tap, a tap while offline, a re-tap while saving', async () => {
      // Finish + Start Next left bout 2 running in the same editor.
      const editor = inlineEditor(page);
      await expect.poll(() => sides(editor)).not.toEqual({ shiro: bout1.shiro, aka: bout1.aka });
      const pair = await sides(editor);

      const neighbour = await tapNeighbour(ipponButton(page, 'shiro', 'M'), 'right');
      await settled(page);
      record({ variant: 'V1 tapNeighbour', action: 'Shiro M', ...neighbour });

      let unsentOffline;
      const offline = await interrupt(page, 'offline', {
        during: async (p) => {
          await ipponButton(p, 'aka', 'M').tap();
          // The outbox still holds the point while it cannot be saved.
          await expect.poll(() => unsentCount(p)).toBeGreaterThan(0);
          unsentOffline = await unsentCount(p);
        },
      });
      const resynced = await settled(page).then(() => true, () => false);
      record({ variant: 'V3 interrupt', action: 'Aka M tapped offline', ...offline, unsent: unsentOffline, resynced });
      await shot(page, 'bout-2-after-offline');

      const armed = await armFinish(page);
      const retap = await retapWhileSaving(armed);
      const finished = await lastCompleted(page);
      record({ variant: 'V2 retapWhileSaving', action: 'Finish (armed)', ...retap,
        completedIsBout2: finished.shiro === pair.shiro && finished.aka === pair.aka, result: finished.result });
      await shot(page, 'bout-2-finished');
    });

    // The overlay editor, the second place the score editor mounts, is the
    // admin pages' and they are for correcting mistakes, never for scoring a
    // competition (operator ruling 2026-11-27): the court of shiaijo B scores
    // its bout, and the Scores tab corrects it.
    let pairB;
    await test.step('shiaijo B: the court scores a bout', async () => {
      await openShiaijo(page, 'B');
      pairB = await startUpNext(page);
      await awardIppon(page, 'aka', 'K');
      await finishMatch(page, INLINE_EDITOR);
      await shot(page, 'shiaijo-B-finished');
    });

    await test.step('shiaijo B corrected from the Scores tab: the overlay editor', async () => {
      // The court entered Aka's K; the referees had given men. Correct: K off, M on.
      await page.goto(`/admin/competition/${encodeURIComponent(compId)}/scores`);
      const row = scoreRows(page).filter({ hasText: pairB.shiro }).filter({ hasText: pairB.aka }).first();
      await row.getByRole('button', { name: /^Correct$/ }).tap();
      const ed = page.locator(EDITOR);
      await expect(ed).toBeVisible();
      await ed.locator('[aria-label^="Aka slot "][aria-label$=": remove K"]').first().tap();
      await expect(filledSlots(page, 'aka', EDITOR)).toHaveCount(0);
      await awardIppon(page, 'aka', 'M', EDITOR);
      await shot(page, 'overlay-editor-B');
      const answered = nextMatchWrite(page);
      await ed.getByRole('button', { name: 'Save correction', exact: true }).tap();
      const reason = page.locator('.reason-prompt').last();
      await expect(reason).toBeVisible();
      await reason.getByRole('textbox').fill('Aka scored men, not kote');
      await reason.getByRole('button', { name: 'Confirm', exact: true }).tap();
      const body = await (await writeLanded(answered, 'the correction')).json();
      expect(body.applied, `the correction was applied: ${JSON.stringify(body)}`).not.toBe(false);
      await expect(ed, 'the editor closes on the saved correction').toHaveCount(0);
      const score = row.locator('.score-edit-row__scoreval');
      await expect(score, 'the row reads the corrected point').toContainText('M');
      await expect(score).not.toContainText('K');
      record({ variant: 'correction', action: 'Aka K corrected to M from the Scores tab', pair: pairB,
        score: (await score.innerText()).replace(/\s+/g, ' ').trim() });
      await shot(page, 'overlay-correction-saved-B');
    });

    await test.step('a spectator phone sees bout 1 on the viewer and the TV board', async () => {
      const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
      try {
        const phone = await spectator.newPage();
        await openViewer(phone, compId);
        await expect(recentResult(phone, bout1)).toContainText(bout1.result);
        await shot(phone, 'viewer-recent-results', { fullPage: true });
        await openTvBoard(phone, 'A');
        await expect(tvBoard(phone)).toContainText(bout1.shiro);
        await shot(phone, 'tv-board-A');
      } finally {
        await spectator.close();
      }
    });
  });
});
