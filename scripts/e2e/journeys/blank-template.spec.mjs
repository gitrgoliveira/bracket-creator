// Blank template journey: the workbook the Export & print tab's "Download
// blank template (.xlsx)" hands an organiser holds the pools the app runs.
//
// Once a competition is drawn, the template is its stored draw
// (GET /api/competitions/:id/export). Before the draw there is nothing stored,
// and the button posts the roster to the stateless generator (POST /create),
// which draws it afresh in registration order from the whole roster. The two
// doors give the same pools for a roster everyone turned up for: the app's
// draw keeps registration order too (measured: twelve entrants, no check-in,
// identical Pool Draw sheets). They part where the app's draw leaves someone
// out. Here check-in leaves two early entrants out, so the stored draw has
// ten competitors in other pools, while a template drawn again from the
// roster still holds all twelve. The journey downloads that counterfactual
// before the draw (the /create door, step 1) and checks the app's pools differ
// from it, so a template that took the old door after the draw cannot pass.
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
  bulkCheckInDojo, deskHeadline, generateDrawExcluding, groupByDojo, openCompetitionTab, openDesk, poolMembers,
  toggleCheckIn,
} from '../fixtures/roster.mjs';
import { downloadTemplateWorkbook, poolDrawMembers } from '../fixtures/organiser.mjs';

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

test.describe('blank template', () => {
  test.afterAll(() => audit.flush());

  test('the blank template of a drawn competition holds the pools the app runs', async ({ page }) => {
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

    let redrawn;
    await test.step('before the draw, the template is drawn from the roster (/create)', async () => {
      const dl = await downloadTemplateWorkbook(page, id);
      await shot(page, 'export-before-draw');
      redrawn = poolDrawMembers(dl.bytes);
      record({ journey: 'blank-template', step: 'template before the draw', routes: dl.routes, pools: redrawn });
      expect(dl.routes, 'a competition in setup has no stored draw to print').toEqual(['POST /create']);
      const names = Object.values(redrawn).flat();
      for (const [name] of ROSTER) expect(names, `${name} is in a template drawn from the whole roster`).toContain(name);
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
      // The guard on the journey itself: a template drawn again from the
      // roster would not match, so the comparison below can tell the doors apart.
      expect(pools, 'the app runs other pools than a draw of the whole roster in registration order').not.toEqual(redrawn);
    });

    await test.step('the blank template of the drawn competition holds those pools', async () => {
      const dl = await downloadTemplateWorkbook(page, id);
      await shot(page, 'export-after-draw');
      const book = poolDrawMembers(dl.bytes);
      record({ journey: 'blank-template', step: 'template after the draw', file: dl.name, routes: dl.routes, pools: book });
      expect(dl.name).toBe(`bracket-${id}.xlsx`);
      // Soft, so a template from the wrong door also reports the pools it holds.
      expect.soft(dl.routes, 'a drawn competition prints its stored draw, never a fresh one').toEqual([`GET /api/competitions/${id}/export`]);
      expect(book, 'the Pool Draw sheet holds the app\'s pools, each in the app\'s order').toEqual(pools);
    });
  });
});
