// Engi (kata pairs) journey: a knockout of four pairs, scored in flags.
//
// The tournament, the engi competition (the wizard's "Engi (kata competition)"
// box), the roster of pairs ("Name 1 - Name 2, Dojo"), the draw and the start
// are all seeded through the interface, once, in beforeAll. The tests then
// walk the court console: one scores a match and checks the result on a
// spectator's phone, the other guards bc-evts, the engi editor's counterpart
// of bc-ltap (a flag taken back while the save of the one before is still out
// must stay taken back).
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE, EXPECT_TIMEOUT_MS } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { openShiaijo, completedRows, upNextCard } from '../fixtures/shiaijo.mjs';
import { openViewer } from '../fixtures/public.mjs';
import { holdScoreWrites } from '../fixtures/held-write.mjs';
import {
  engiEditor, engiSettle, engiTotal, enterFlags, flagCount, flagButton, saveEngiResult, startUpNextEngi,
} from '../fixtures/engi.mjs';

// Four pairs, one dojo each: (name, dojo) is unique per pair.
const PAIRS = [
  'Akira Tanaka - Ben Carter, Gyokusen',
  'Chie Mori - Dan Evans, Musashi',
  'Eri Kato - Finn Hale, Seishinkan',
  'Goro Abe - Hana Ito, Thames',
];

let compId;
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
    expect(rowText, 'Aka, who held the two flags, is marked the winner').toContain(`Aka: ✓ Winner: ${pair.aka}`);
    expect(rowText.match(/✓/g), 'and only Aka').toHaveLength(1);
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
      expect(cardText, 'Aka is marked the winner').toMatch(new RegExp(`Aka: ${pair.aka}[^✓]*✓`));
      expect(cardText.match(/✓/g), 'and only Aka').toHaveLength(1);
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
});
