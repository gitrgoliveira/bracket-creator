// Blank template journey: the workbook the Export & print tab's "Download
// blank template (.xlsx)" hands an organiser.
//
// The button always takes one door, GET /api/competitions/:id/export. Once a
// competition is drawn, that is its stored draw, holding the pools the app runs.
// Before the draw there is nothing stored, and the answer is the blank layout
// the draw would print for the expected number of places, every place empty: no
// names, so nobody is left out or put in a pool the draw has not made. The
// journey downloads it before the draw (empty places, sized from the roster),
// then after the draw (the app's own pools, from the ten competitors checked in).
//
// The Export tab around the button is read too: before the draw it says only a
// blank template can be downloaded and warns that the list was never shuffled
// (Shuffle unseeded on Participants clears the warning), and "Download results"
// is offered only once a match has a recorded result.
//
// Everything is seeded through the interface: the wizard, the roster paste
// box, the Registration desk and Generate draw.
import { test, expect } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { enterTournament } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { pasteRoster } from '../fixtures/competition.mjs';
import {
  bulkCheckInDojo, deskHeadline, generateDrawExcluding, groupByDojo, openCompetitionTab, openDesk, openParticipants,
  poolMembers, toggleCheckIn,
} from '../fixtures/roster.mjs';
import { openShiaijo, upNextCard } from '../fixtures/shiaijo.mjs';
import { settled } from '../fixtures/pace.mjs';
import { FIRST_RESULT_CONFIRM_LABEL } from '../../screenshots/lib/editor.mjs';
import { playRunningBout } from '../fixtures/individual.mjs';
import { downloadResultsWorkbook, downloadTemplateWorkbook, poolDrawMembers, readWorkbook } from '../fixtures/organiser.mjs';

const COMP = 'Template Open';
// Twelve entrants, six dojos of two, (name, dojo) unique. Pools of three.
const ROSTER = [
  ['Aoi Sato', 'Gyokusen'], ['Gen Ota', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Tom Hill', 'Thames'],
  ['Chie Mori', 'Musashi'], ['Mei Ono', 'Musashi'], ['Dan Evans', 'Seishinkan'], ['Sho Ito', 'Seishinkan'],
  ['Eri Kato', 'Kenshikan'], ['Fumi Yamada', 'Kenshikan'], ['Hana Ito', 'Shudokan'], ['Ino Mizuno', 'Shudokan'],
];
// The two who never arrive: the first entrants of Gyokusen and Thames, so
// every pool after the first shifts when they are left out.
const ABSENT = ['Aoi Sato', 'Ben Carter'];

const audit = auditLog('blank-template');
const { record } = audit;

// The Export tab's own parts: the notice before the draw, the warning that the
// list was never shuffled, and the two download buttons.
const preDrawNotice = (page) => page.getByTestId('export-pre-draw-notice');
const notShuffled = (page) => page.getByTestId('export-not-shuffled');
const resultsButton = (page) => page.getByRole('button', { name: 'Download results (.xlsx)' });
const templateButton = (page) => page.getByRole('button', { name: 'Download blank template (.xlsx)' });

test.describe('blank template', () => {
  test.afterAll(() => audit.flush());

  test('the Export tab offers the blank layout before the draw and the stored draw after it', async ({ page }) => {
    const shot = journeyShots('blank-template');
    await enterTournament(page, { name: 'Template Cup' });
    const id = await createCompetition(page, {
      name: COMP, kind: 'individual', format: 'mixed', poolSize: 3, courts: ['A'], checkIn: true,
    });
    await pasteRoster(page, id, ROSTER);

    await test.step('check in everyone but two early entrants', async () => {
      await openDesk(page, COMP);
      await groupByDojo(page, true);
      for (const dojo of ['Musashi', 'Seishinkan', 'Kenshikan', 'Shudokan']) await bulkCheckInDojo(page, dojo);
      await toggleCheckIn(page, 'Gen Ota');
      await toggleCheckIn(page, 'Tom Hill');
      await expect(deskHeadline(page)).toContainText('10 of 12');
    });

    await test.step('before the draw, the template is the blank layout (empty places)', async () => {
      const dl = await downloadTemplateWorkbook(page, id);
      await expect(preDrawNotice(page), 'the tab says only a blank template is offered').toContainText('only a blank template can be downloaded');
      await expect(notShuffled(page), 'the list has never been shuffled').toContainText('never been shuffled');
      await expect(resultsButton(page), 'nothing is recorded, so no results workbook').toHaveCount(0);
      await expect(templateButton(page)).toBeVisible();
      await shot(page, 'export-before-draw');
      const blank = poolDrawMembers(dl.bytes);
      record({ journey: 'blank-template', step: 'template before the draw', routes: dl.routes, pools: blank });
      expect(dl.routes, 'the one door, before and after the draw').toEqual([`GET /api/competitions/${id}/export`]);
      const pools = Object.values(blank);
      expect(pools, 'twelve places in pools of three').toHaveLength(4);
      for (const places of pools) expect(places, 'three places a pool').toHaveLength(3);
      for (const name of Object.values(blank).flat()) expect(name, 'a place names nobody').toBeFalsy();
    });

    await test.step('Shuffle unseeded on Participants clears the never-shuffled warning', async () => {
      await openParticipants(page, id);
      const saved = page.waitForResponse((r) => r.request().method() === 'PUT'
        && new URL(r.url()).pathname.startsWith(`/api/competitions/${id}`));
      await page.getByRole('button', { name: 'Shuffle unseeded' }).tap();
      expect((await saved).ok(), 'the shuffled list was saved').toBe(true);
      await page.goto(`/admin/competition/${id}/export`);
      await expect(templateButton(page)).toBeVisible();
      await expect(preDrawNotice(page), 'still no draw').toBeVisible();
      await expect(notShuffled(page), 'a shuffle is recorded').toHaveCount(0);
    });

    let pools;
    await test.step('generate the draw and read its pools on the Pools tab', async () => {
      await generateDrawExcluding(page, id, ABSENT.length);
      await openCompetitionTab(page, id, 'pools');
      pools = await poolMembers(page);
      await shot(page, 'pools-drawn');
      record({ journey: 'blank-template', step: 'app pools', pools });
      const drawn = Object.values(pools).flat();
      expect(drawn, 'only the ten checked in are drawn').toHaveLength(ROSTER.length - ABSENT.length);
      for (const name of ABSENT) expect(drawn, `${name} is not drawn`).not.toContain(name);
    });

    await test.step('the blank template of the drawn competition holds those pools', async () => {
      const dl = await downloadTemplateWorkbook(page, id);
      await shot(page, 'export-after-draw');
      const book = poolDrawMembers(dl.bytes);
      record({ journey: 'blank-template', step: 'template after the draw', file: dl.name, routes: dl.routes, pools: book });
      expect(dl.name).toBe(`bracket-${id}.xlsx`);
      // Soft, so a template from the wrong door also reports the pools it holds.
      expect.soft(dl.routes, 'a drawn competition prints its stored draw').toEqual([`GET /api/competitions/${id}/export`]);
      expect(book, 'the Pool Draw sheet holds the app\'s pools, each in the app\'s order').toEqual(pools);
      await expect(preDrawNotice(page), 'a drawn competition has no pre-draw notice').toHaveCount(0);
      await expect(resultsButton(page), 'a draw alone records nothing').toHaveCount(0);
    });

    await test.step('once a bout is recorded the results workbook is offered', async () => {
      await openShiaijo(page, 'A');
      // The competition is still draw-ready, so its first start asks to start
      // the competition before anything is sent; the operator agrees.
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      const ask = page.locator('.modal-backdrop--dialog [role="dialog"]');
      await expect(ask, 'the first result of a draw-ready competition asks to start it').toContainText(`Start ${COMP}?`);
      const agree = ask.getByRole('button', { name: FIRST_RESULT_CONFIRM_LABEL });
      await settled(agree);
      await agree.tap();
      const pair = await playRunningBout(page);
      record({ journey: 'blank-template', step: 'bout recorded', pair });
      await page.goto(`/admin/competition/${id}/export`);
      await expect(resultsButton(page)).toBeVisible();
      await expect(templateButton(page)).toBeVisible();
      await shot(page, 'export-with-result');
      const dl = await downloadResultsWorkbook(page, id);
      expect(dl.name).toBe(`results-${id}.xlsx`);
      expect(readWorkbook(dl.bytes).sheets, 'the results workbook keeps the Pool Draw sheet').toContain('Pool Draw');
    });
  });
});
