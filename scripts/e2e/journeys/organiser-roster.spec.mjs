// Organiser roster journey: what an organiser does with the roster around the
// draw, in an officiated tournament, all through the interface.
//
//   1. Check-in on the Registration desk (one by one, by search, by dojo) and
//      the draw that follows: only checked-in competitors are drawn, and the
//      desk's search finds a competitor by number.
//   2. Seeds typed before the draw: two seeds never share a pool.
//   3. The participant Edit dialog after the draw: a new name and dojo reach
//      the court console, the bracket and the public viewer; the same name and
//      dojo as another competitor is refused, the same name in another dojo is
//      accepted.
//   4. Replacing a drawn competitor who has not fought with someone new: the
//      newcomer takes the same slot, the old name is nowhere.
//
// One server for the file (serial); the tournament is made once, in beforeAll,
// and every test seeds its own competition so it reads as a story of its own.
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster } from '../fixtures/competition.mjs';
import { openShiaijo } from '../fixtures/shiaijo.mjs';
import { openViewer } from '../fixtures/public.mjs';
import {
  openDesk, deskRows, deskRow, deskCheck, deskHeadline, deskSearch, toggleCheckIn, searchDesk,
  groupByDojo, bulkCheckInDojo, openParticipants, setSeed, seedRankOf, openEdit, saveEdit,
  editDialog, toastText, poolMembers, bracketSlots, bracketNames, openCompetitionTab, generateDrawExcluding,
} from '../fixtures/roster.mjs';

// Twelve entrants, four dojos of three, (name, dojo) unique.
const ROSTER_12 = [
  ['Aoi Sato', 'Gyokusen'], ['Gen Ota', 'Gyokusen'], ['Goro Abe', 'Gyokusen'],
  ['Ben Carter', 'Thames'], ['Tom Hill', 'Thames'], ['Tia Moss', 'Thames'],
  ['Chie Mori', 'Musashi'], ['Mei Ono', 'Musashi'], ['Max Reed', 'Musashi'],
  ['Dan Evans', 'Seishinkan'], ['Sho Ito', 'Seishinkan'], ['Sia Lowe', 'Seishinkan'],
];
// Eight entrants, four dojos of two.
const ROSTER_8 = [
  ['Akira Tanaka', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Chie Mori', 'Musashi'],
  ['Dan Evans', 'Seishinkan'], ['Eri Kato', 'Gyokusen'], ['Finn Hale', 'Thames'],
  ['Goro Abe', 'Musashi'], ['Hana Ito', 'Seishinkan'],
];

const audit = auditLog('organiser-roster');
const { record } = audit;
let shot;
const ids = {};

test.describe.configure({ mode: 'serial' });

test.describe('organiser roster', () => {
  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    shot = journeyShots('organiser-roster');
    const server = await serverForFile(testInfo.file);
    const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: server.base });
    try {
      const page = await ctx.newPage();
      await createTournament(page, { name: 'Roster Cup', courts: 2 });
    } finally {
      await ctx.close();
    }
  });

  test.afterAll(() => audit.flush());

  test('check-in decides who is drawn, and the desk finds a number', async ({ page }) => {
    await login(page);
    ids.checkIn = await createCompetition(page, {
      name: 'Check-in Cup', kind: 'individual', format: 'knockout', courts: ['A'], numberPrefix: 'K', checkIn: true,
    });
    await pasteRoster(page, ids.checkIn, ROSTER_12);

    await openDesk(page, 'Check-in Cup');
    await expect(deskRows(page)).toHaveCount(12);
    await expect(deskHeadline(page)).toContainText('0 of 12');
    await shot(page, 'desk-nobody-in');

    // One at a time, from the row's own box.
    await toggleCheckIn(page, 'Aoi Sato');
    // By search: Enter checks in the top match.
    await searchDesk(page, 'Tom Hill');
    await expect(deskRows(page)).toHaveCount(1);
    await deskSearch(page).press('Enter');
    await expect(deskSearch(page)).toHaveValue('');
    await expect(deskCheck(page, 'Tom Hill')).toHaveAttribute('aria-checked', 'true');
    await expect(deskHeadline(page)).toContainText('2 of 12');

    // In bulk: a whole dojo from its group header. Thames is left as it is, so
    // two Thames competitors (Ben Carter, Tia Moss) never arrive.
    await groupByDojo(page, true);
    for (const dojo of ['Gyokusen', 'Musashi', 'Seishinkan']) await bulkCheckInDojo(page, dojo);
    await expect(deskHeadline(page)).toContainText('10 of 12');
    await shot(page, 'desk-ten-in');

    // "Unchecked only" leaves exactly the two who have not arrived.
    await page.getByRole('button', { name: 'Unchecked only' }).tap();
    await expect(deskRows(page)).toHaveCount(2);
    await expect(deskRow(page, 'Ben Carter')).toHaveCount(1);
    await expect(deskRow(page, 'Tia Moss')).toHaveCount(1);
    record({ journey: 'organiser-roster', action: 'check-in', checkedIn: 10, absent: ['Ben Carter', 'Tia Moss'] });

    // The draw takes only the ten who are checked in.
    const asked = await generateDrawExcluding(page, ids.checkIn, 2);
    record({ journey: 'organiser-roster', action: 'generate draw confirm', asked });
    await openCompetitionTab(page, ids.checkIn, 'bracket');
    const names = await bracketNames(page);
    const absent = ['Ben Carter', 'Tia Moss'];
    const drawn = ROSTER_12.map(([n]) => n).filter((n) => !absent.includes(n));
    expect([...names].sort(), 'the bracket holds exactly the checked-in competitors').toEqual([...drawn].sort());
    await shot(page, 'bracket-checked-in-only');

    // The desk's number search: bare digits find the competitor whose number
    // contains them. Ten were drawn, so K1..K10; "10" is K10 alone.
    await openDesk(page, 'Check-in Cup');
    await searchDesk(page, '10');
    await expect(deskRows(page)).toHaveCount(1);
    await expect(deskRows(page).first().locator('.rd-tag--number .rd-tag__value')).toHaveText('K10');
    const found = (await deskRows(page).first().locator('.rd-row__name').innerText()).trim();
    expect(drawn, 'the K10 row is one of the drawn').toContain(found);
    record({ journey: 'organiser-roster', action: 'desk number search', query: '10', found });
    await shot(page, 'desk-search-number');
    // The two who were left out carry no number.
    await searchDesk(page, '');
    await expect(deskRow(page, 'Ben Carter').locator('.rd-tag--number')).toHaveCount(0);
    await expect(deskRows(page).locator('.rd-tag--number')).toHaveCount(10);
  });

  test('seeds typed before the draw never share a pool', async ({ page }) => {
    await login(page);
    ids.seeds = await createCompetition(page, {
      name: 'Seed Cup', kind: 'individual', format: 'mixed', poolSize: 4, poolWinners: 2, courts: ['A'], numberPrefix: 'S',
    });
    await pasteRoster(page, ids.seeds, ROSTER_8);

    // The first two names on the roster: the order a plain draw would put
    // together, which is what makes the rule visible.
    await openParticipants(page, ids.seeds);
    await setSeed(page, 'Akira Tanaka', 1);
    await setSeed(page, 'Ben Carter', 2);
    // Each rank is saved as it is typed: they survive a reload.
    await page.reload();
    await expect(page.getByRole('button', { name: 'Clear seeds' })).toBeVisible();
    expect(await seedRankOf(page, 'Akira Tanaka')).toBe('1');
    expect(await seedRankOf(page, 'Ben Carter')).toBe('2');
    await shot(page, 'seeds-typed');

    await generateDraw(page, ids.seeds);
    await openCompetitionTab(page, ids.seeds, 'pools');
    const pools = await poolMembers(page);
    const poolOf = (n) => Object.keys(pools).find((p) => pools[p].includes(n));
    record({ journey: 'organiser-roster', action: 'seeds placed', pools });
    expect(Object.keys(pools), 'two pools of four').toHaveLength(2);
    expect(poolOf('Akira Tanaka'), 'seed 1 is drawn').toBeTruthy();
    expect(poolOf('Ben Carter'), 'seed 2 is drawn').toBeTruthy();
    expect(poolOf('Akira Tanaka'), 'two seeds never share a pool').not.toBe(poolOf('Ben Carter'));
    await shot(page, 'pools-seeds-apart');
  });

  test('an edited name and dojo reach the court, the bracket and the public viewer; duplicates are refused', async ({ page, browser, baseURL }) => {
    await login(page);
    ids.edit = await createCompetition(page, {
      name: 'Edit Cup', kind: 'individual', format: 'knockout', courts: ['B'], numberPrefix: 'E',
    });
    await pasteRoster(page, ids.edit, ROSTER_8);
    await generateDraw(page, ids.edit);
    await openCompetitionTab(page, ids.edit, 'bracket');
    const before = await bracketSlots(page);
    const slotOf = (slots, n) => Object.keys(slots).find((k) => slots[k].name === n);
    const danSlot = slotOf(before, 'Dan Evans');
    expect(danSlot, 'Dan Evans is drawn').toBeTruthy();

    // A new name and a new dojo, after the draw.
    await openParticipants(page, ids.edit);
    await openEdit(page, 'Dan Evans');
    await saveEdit(page, { name: 'Daniel Evans', dojo: 'Seishinkan UK' });
    await expect(editDialog(page)).toHaveCount(0);
    await shot(page, 'participants-after-edit');

    const expectEdited = (slots, where) => {
      expect(slots[danSlot], `${where}: the same slot now holds Daniel Evans`).toMatchObject({
        name: 'Daniel Evans', dojo: 'Seishinkan UK', num: before[danSlot].num,
      });
      expect(Object.values(slots).map((s) => s.name), `${where}: Dan Evans is nowhere`).not.toContain('Dan Evans');
    };
    await openCompetitionTab(page, ids.edit, 'bracket');
    expectEdited(await bracketSlots(page), 'admin bracket');
    await shot(page, 'bracket-after-edit');

    await openShiaijo(page, 'B');
    const queue = page.locator('.shiaijo__queue');
    await expect(queue.locator('.numbered-name__text', { hasText: 'Daniel Evans' }).first()).toBeVisible();
    const consoleNames = await queue.locator('.numbered-name__text').allInnerTexts();
    expect(consoleNames.map((n) => n.trim()), 'court console: Dan Evans is nowhere').not.toContain('Dan Evans');
    await shot(page, 'court-after-edit');

    const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    try {
      const phone = await spectator.newPage();
      await openViewer(phone, ids.edit);
      await phone.getByRole('button', { name: 'Bracket' }).tap();
      expectEdited(await bracketSlots(phone), 'public viewer');
      await shot(phone, 'viewer-after-edit', { fullPage: true });
    } finally {
      await spectator.close();
    }

    // Same name AND same dojo as another competitor: refused, with its reason.
    await openParticipants(page, ids.edit);
    await openEdit(page, 'Finn Hale');
    await saveEdit(page, { name: 'Akira Tanaka', dojo: 'Gyokusen' });
    const refusal = await toastText(page, /same name and dojo/i);
    record({ journey: 'organiser-roster', action: 'duplicate refused', message: refusal });
    await expect(editDialog(page), 'the dialog stays open on a refusal').toBeVisible();
    await shot(page, 'edit-duplicate-refused');

    // The same name in a different dojo is two different people: accepted.
    await saveEdit(page, { dojo: 'Thames' });
    await expect(editDialog(page)).toHaveCount(0);
    await openCompetitionTab(page, ids.edit, 'bracket');
    const after = await bracketSlots(page);
    const akiras = Object.values(after).filter((s) => s.name === 'Akira Tanaka').map((s) => s.dojo).sort();
    expect(akiras, 'two Akira Tanaka, told apart by dojo').toEqual(['Gyokusen', 'Thames']);
    expect(Object.values(after).map((s) => s.name), 'Finn Hale was renamed, not duplicated').not.toContain('Finn Hale');
    await shot(page, 'bracket-namesakes');
  });

  test('replacing a drawn competitor who has not fought puts the newcomer in the same slot', async ({ page, browser, baseURL }) => {
    await login(page);
    await openCompetitionTab(page, ids.edit, 'bracket');
    const before = await bracketSlots(page);
    const slot = Object.keys(before).find((k) => before[k].name === 'Hana Ito');
    expect(slot, 'Hana Ito is drawn and has not fought').toBeTruthy();

    await openParticipants(page, ids.edit);
    await openEdit(page, 'Hana Ito');
    await saveEdit(page, { name: 'Zed Newman', dojo: 'Shudokan' });
    await expect(editDialog(page)).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Edit Zed Newman' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Edit Hana Ito' })).toHaveCount(0);

    const expectReplaced = (slots, where) => {
      expect(slots[slot], `${where}: the newcomer holds the old slot, with its number`).toMatchObject({
        name: 'Zed Newman', dojo: 'Shudokan', num: before[slot].num,
      });
      expect(Object.values(slots).map((s) => s.name), `${where}: Hana Ito is nowhere`).not.toContain('Hana Ito');
      // Nobody else moved.
      Object.keys(before).filter((k) => k !== slot && before[k].name !== 'Finn Hale').forEach((k) => {
        expect(slots[k]?.num, `${where}: ${k} keeps its number`).toBe(before[k].num);
      });
    };
    await openCompetitionTab(page, ids.edit, 'bracket');
    expectReplaced(await bracketSlots(page), 'admin bracket');
    await shot(page, 'bracket-after-replace');

    await openShiaijo(page, 'B');
    const consoleNames = (await page.locator('.shiaijo__queue .numbered-name__text').allInnerTexts()).map((n) => n.trim());
    expect(consoleNames, 'court console shows Zed Newman').toContain('Zed Newman');
    expect(consoleNames, 'court console: Hana Ito is nowhere').not.toContain('Hana Ito');
    await shot(page, 'court-after-replace');

    const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    try {
      const phone = await spectator.newPage();
      await openViewer(phone, ids.edit);
      await phone.getByRole('button', { name: 'Bracket' }).tap();
      expectReplaced(await bracketSlots(phone), 'public viewer');
    } finally {
      await spectator.close();
    }
    record({ journey: 'organiser-roster', action: 'replace', slot, replaced: before[slot] });
  });
});
