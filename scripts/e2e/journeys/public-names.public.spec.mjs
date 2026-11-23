// Competitor names hidden on the public pages, on the PUBLIC project (a
// 390x844 phone, no sign-in).
//
// The organiser hides the names of an officiated competition from its settings
// page; a spectator's phone then sees competitor numbers and dojos, never a
// name, on the public home, the competition page and its tabs, the schedule and
// the match card. A competitor's own reveal link shows that competitor's name
// alone, and a competition link every name of that competition. A self-run
// tournament offers no such setting and shows every name.
//
// The test's own `page` is a phone that never opened a link: each test gets a
// fresh one, and every reveal link is opened in a phone context of its own, so
// a key one phone keeps never reaches another. The tournament is seeded from a
// SECOND context with the operator's iPad (OPERATOR_DEVICE), through the
// interface; that context may read the clipboard, which is where the app's
// Copy buttons put a reveal link.
//
// Two individual competitions:
//   J  Junior Open  6 competitors, two pools of 3, court A: names shown at
//      first, then hidden by the organiser from the settings page
//   C  Cadet Open   4 competitors, knockout, court B: created with "Numbers
//      only" from the Add competition form, so it never shows a name
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { createTournament } from '../fixtures/setup.mjs';
import { PUBLIC_NAMES, choosePublicNames, createCompetition, publicNamesGroup } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import {
  competitionTab, matchCard, namesHiddenNote, namesOnPage, numbersOf, numbersOnPage, openRevealLink, openViewer,
  revealPage, scheduleRows,
} from '../fixtures/public.mjs';
import { start } from '../../screenshots/lib/server.mjs';

// No name holds a dojo's word, or the other way round, so a dojo on the page
// never reads as a name.
const JUNIORS = [
  ['Akira Tanaka', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Chie Mori', 'Musashi'],
  ['Dan Evans', 'Seishinkan'], ['Eri Kato', 'Hizen'], ['Finn Hale', 'Mumeishi'],
];
const CADETS = [
  ['Gina Holt', 'Bristol'], ['Hugo Lane', 'Nottingham'], ['Iris Moss', 'Oxford'], ['Kai Ward', 'Leeds'],
];
const names = (rows) => rows.map(([n]) => n);
const dojos = (rows) => rows.map(([, d]) => d);
const JUNIOR_NAMES = names(JUNIORS);
const CADET_NAMES = names(CADETS);
const EVERY_NAME = [...JUNIOR_NAMES, ...CADET_NAMES];

// A reveal link as the Copy buttons build it: /reveal/<competition>#<key>, the
// key a hex HMAC.
const REVEAL_LINK = /\/reveal\/[^/#]+#[0-9a-f]{64}$/;

const settingsPath = (id) => `/admin/competition/${id}/settings`;

// Pick the option on the competition's settings page and save it. A save that
// lands takes the page back to the dashboard, so the settings page is opened
// again afterwards, where the saved option and its links show.
async function setPublicNames(op, id, value) {
  await op.goto(settingsPath(id));
  await expect(publicNamesGroup(op)).toBeVisible();
  await choosePublicNames(op, value);
  await op.getByRole('button', { name: 'Save changes' }).first().tap();
  await expect(op).not.toHaveURL(new RegExp(`${settingsPath(id)}$`));
  await op.goto(settingsPath(id));
  await expect(publicNamesGroup(op).getByRole('button', { name: PUBLIC_NAMES[value], exact: true }))
    .toHaveAttribute('aria-pressed', 'true');
}

// Tap a Copy button and return the link it put on the clipboard. The clipboard
// is emptied first, so a link copied earlier is never read as this one.
async function copiedLink(op, button) {
  await op.evaluate(() => navigator.clipboard.writeText(''));
  await button.tap();
  let url = '';
  await expect.poll(async () => {
    url = await op.evaluate(() => navigator.clipboard.readText());
    return url;
  }, { message: 'the Copy button puts a reveal link on the clipboard' }).toMatch(REVEAL_LINK);
  return url;
}

test.describe.configure({ mode: 'serial' });

test.describe('competitor names hidden on the public pages', () => {
  let shot;
  let opCtx;
  let op; // the operator's page, signed in, kept for the whole describe
  let server;
  const ids = {};
  const links = {};
  // Phone contexts the tests open for reveal links, closed after each test.
  let phones = [];
  const phone = async () => {
    const ctx = await opCtx.browser().newContext({ ...PUBLIC_DEVICE, baseURL: server.base });
    phones.push(ctx);
    return ctx.newPage();
  };

  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    shot = journeyShots('public-names');
    server = await serverForFile(testInfo.file);
    opCtx = await browser.newContext({
      ...OPERATOR_DEVICE, baseURL: server.base, permissions: ['clipboard-read', 'clipboard-write'],
    });
    op = await opCtx.newPage();
    await createTournament(op, { name: 'Junior Cup', courts: 2 });
    ids.junior = await createCompetition(op, {
      name: 'Junior Open', kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A'], numberPrefix: 'J',
    });
    await pasteRoster(op, ids.junior, JUNIORS);
    await generateDraw(op, ids.junior);
    await startCompetition(op, ids.junior);
    ids.cadet = await createCompetition(op, {
      name: 'Cadet Open', kind: 'individual', format: 'knockout', courts: ['B'], numberPrefix: 'C', publicNames: 'hidden',
    });
    await pasteRoster(op, ids.cadet, CADETS);
    await generateDraw(op, ids.cadet);
    await startCompetition(op, ids.cadet);
  });

  test.afterEach(async () => {
    for (const ctx of phones) await ctx.close();
    phones = [];
  });

  test.afterAll(async () => {
    await opCtx?.close();
  });

  test('a competition created with "Numbers only" shows numbers; one that shows names names everyone', async ({ page }) => {
    await test.step('Junior Open still shows names: every competitor is on its Pools tab', async () => {
      await openViewer(page, ids.junior);
      await competitionTab(page, 'Pools').tap();
      await expect(page.locator('.pool-match-numbered-row')).toHaveCount(6);
      expect(await namesOnPage(page, JUNIOR_NAMES)).toEqual(JUNIOR_NAMES);
      await expect(namesHiddenNote(page)).toHaveCount(0);
      await shot(page, 'junior-names-shown');
    });

    await test.step('Cadet Open, set to "Numbers only" in the Add competition form, shows numbers and dojos', async () => {
      await openViewer(page, ids.cadet);
      await expect(namesHiddenNote(page), 'the page says its names are hidden').toBeVisible();
      await competitionTab(page, 'Bracket').tap();
      await expect.poll(() => numbersOnPage(page, 'C'), { message: 'the bracket shows every cadet by number' })
        .toEqual(numbersOf('C', 4));
      expect(await namesOnPage(page, EVERY_NAME), 'no name on the Cadet Open page').toEqual([]);
      await shot(page, 'cadet-numbers-only');
    });

    await test.step('"Numbers only" gives the organiser no link to hand out', async () => {
      await op.goto(settingsPath(ids.cadet));
      await expect(publicNamesGroup(op).getByRole('button', { name: 'Numbers only', exact: true })).toHaveAttribute('aria-pressed', 'true');
      await expect(op.getByTestId('reveal-links-panel')).toHaveCount(0);
      await expect(op.getByTestId('reveal-links-person')).toHaveCount(0);
    });
  });

  test('the organiser hides Junior Open\'s names: home, competition page, schedule and match card show numbers and dojos only', async ({ page }) => {
    await test.step('the organiser picks "Numbers, a link per person" on the settings page and saves', async () => {
      await setPublicNames(op, ids.junior, 'person');
      await expect(op.getByTestId('reveal-links-person')).toBeVisible();
      await shot(op, 'organiser-settings-person');
    });

    await test.step('the public home names nobody', async () => {
      await page.goto('/');
      await expect(page.locator('.viewer__title')).toHaveText('Junior Cup');
      await expect(page.locator('.vlist-item--comp', { hasText: 'Junior Open' })).toContainText('6 players');
      await expect(page.locator('button.vsched-item').first()).toContainText(/[JC]\d+/);
      expect(await namesOnPage(page, EVERY_NAME), 'no name on the home').toEqual([]);
      await shot(page, 'home', { fullPage: true });
    });

    await test.step('the competition page: Overview, Pools and Bracket name nobody; the Pools tab shows every number and dojo', async () => {
      await openViewer(page, ids.junior);
      await expect(namesHiddenNote(page), 'the page says its names are hidden').toBeVisible();
      expect(await namesOnPage(page, EVERY_NAME), 'no name on the Overview').toEqual([]);
      await competitionTab(page, 'Pools').tap();
      await expect(page.locator('.pool-match-numbered-row')).toHaveCount(6);
      await expect.poll(() => numbersOnPage(page, 'J'), { message: 'the Pools tab shows every junior by number' })
        .toEqual(numbersOf('J', 6));
      expect(await namesOnPage(page, dojos(JUNIORS)), 'the Pools tab shows every dojo').toEqual(dojos(JUNIORS));
      expect(await namesOnPage(page, EVERY_NAME), 'no name on the Pools tab').toEqual([]);
      await shot(page, 'competition-pools');
      await competitionTab(page, 'Bracket').tap();
      await expect(competitionTab(page, 'Bracket')).toHaveClass(/is-active/);
      expect(await namesOnPage(page, EVERY_NAME), 'no name on the Bracket tab').toEqual([]);
    });

    await test.step('the match card opened from a pool match names nobody', async () => {
      await competitionTab(page, 'Pools').tap();
      await page.locator('button.pool-match-numbered-row').first().tap();
      const card = matchCard(page);
      await expect(card).toBeVisible();
      await expect(card).toContainText(/J\d+/);
      expect(await namesOnPage(page, EVERY_NAME), 'no name on the match card').toEqual([]);
      await shot(page, 'match-card');
    });

    await test.step('the schedule lists every match and names nobody', async () => {
      await page.goto('/schedule');
      await expect(scheduleRows(page).first()).toBeVisible();
      await expect(scheduleRows(page).first()).toContainText(/[JC]\d+/);
      expect(await namesOnPage(page, EVERY_NAME), 'no name on the schedule').toEqual([]);
      await shot(page, 'schedule', { fullPage: true });
    });
  });

  test('a competitor\'s own reveal link shows that competitor\'s name alone', async ({ page }) => {
    await test.step('the organiser copies Akira Tanaka\'s link from the Participants tab', async () => {
      await op.goto(`/admin/competition/${ids.junior}/participants`);
      links.akira = await copiedLink(op, op.getByRole('button', { name: 'Copy reveal link for Akira Tanaka', exact: true }));
      await expect(op.getByText(/^Reveal link for Akira Tanaka copied/)).toBeVisible();
    });

    await test.step('a phone that opens it sees Akira Tanaka by name and everyone else by number', async () => {
      const parent = await phone();
      await openRevealLink(parent, links.akira);
      await expect(namesHiddenNote(parent), 'the page says its names are hidden').toBeVisible();
      await competitionTab(parent, 'Pools').tap();
      await expect(parent.locator('.pool-match-numbered-row')).toHaveCount(6);
      await expect.poll(() => namesOnPage(parent, EVERY_NAME), { message: 'the link reveals one name' })
        .toEqual(['Akira Tanaka']);
      await shot(parent, 'person-link-pools');
    });

    await test.step('a phone that opened no link still names nobody', async () => {
      await openViewer(page, ids.junior);
      await competitionTab(page, 'Pools').tap();
      await expect(page.locator('.pool-match-numbered-row')).toHaveCount(6);
      expect(await namesOnPage(page, EVERY_NAME)).toEqual([]);
    });
  });

  test('a competition reveal link shows every name of that competition', async ({ page }) => {
    await test.step('the organiser switches Junior Open to one competition link and copies it', async () => {
      await setPublicNames(op, ids.junior, 'competition');
      const panel = op.getByTestId('reveal-links-panel');
      await expect(panel).toBeVisible();
      links.junior = await copiedLink(op, panel.getByRole('button', { name: 'Copy link', exact: true }));
      await expect(op.getByText(/^Competition link copied/)).toBeVisible();
      await shot(op, 'organiser-settings-competition');
    });

    await test.step('a phone that opens it sees every Junior Open name, and no Cadet Open one', async () => {
      const spectator = await phone();
      await openRevealLink(spectator, links.junior);
      await expect(namesHiddenNote(spectator)).toHaveCount(0);
      await competitionTab(spectator, 'Pools').tap();
      await expect(spectator.locator('.pool-match-numbered-row')).toHaveCount(6);
      await expect.poll(() => namesOnPage(spectator, JUNIOR_NAMES), { message: 'the link reveals every junior' })
        .toEqual(JUNIOR_NAMES);
      await shot(spectator, 'competition-link-pools');
      await openViewer(spectator, ids.cadet);
      await competitionTab(spectator, 'Bracket').tap();
      await expect.poll(() => numbersOnPage(spectator, 'C')).toEqual(numbersOf('C', 4));
      expect(await namesOnPage(spectator, EVERY_NAME), 'the Junior Open link reveals nothing of Cadet Open').toEqual([]);
    });

    await test.step('Akira Tanaka\'s own link, of the kind switched off, now shows no names', async () => {
      const parent = await phone();
      await parent.goto(links.akira);
      await expect(revealPage(parent)).toContainText('This link shows no names now');
      await openViewer(parent, ids.junior);
      await competitionTab(parent, 'Pools').tap();
      await expect(parent.locator('.pool-match-numbered-row')).toHaveCount(6);
      expect(await namesOnPage(parent, EVERY_NAME)).toEqual([]);
      await shot(parent, 'person-link-switched-off');
    });

    await test.step('a phone that opened no link still names nobody', async () => {
      await page.goto('/');
      expect(await namesOnPage(page, EVERY_NAME)).toEqual([]);
    });
  });
});

// A self-run tournament shows every name: the setting is not offered at all.
// Its type is fixed when it is created, so it has a server of its own.
test.describe('a self-run tournament', () => {
  let selfRun;
  let ctxs = [];
  const open = async (browser, device) => {
    const ctx = await browser.newContext({ ...device, baseURL: selfRun.base });
    ctxs.push(ctx);
    return ctx.newPage();
  };

  test.beforeAll(async () => {
    selfRun = await start('mobile');
  });

  test.afterAll(async () => {
    for (const ctx of ctxs) await ctx.close();
    ctxs = [];
    await selfRun?.stop();
  });

  test('offers no setting to hide names, and its public pages name everyone', async ({ browser }) => {
    const shot = journeyShots('public-names');
    const op = await open(browser, OPERATOR_DEVICE);
    let id;

    await test.step('the Add competition form and the competition settings offer no "Competitor names on public pages"', async () => {
      await createTournament(op, { name: 'Club Night', courts: 1, mode: 'Self-run' });
      await op.goto('/admin/create-competition');
      await expect(op.getByRole('heading', { level: 1, name: 'Add competition' })).toBeVisible();
      await expect(op.getByRole('button', { name: /^Create & continue/ })).toBeVisible();
      await expect(publicNamesGroup(op)).toHaveCount(0);
      id = await createCompetition(op, {
        name: 'Club Open', kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A'], numberPrefix: 'J',
      });
      await pasteRoster(op, id, JUNIORS);
      await generateDraw(op, id);
      await startCompetition(op, id);
      await op.goto(settingsPath(id));
      await expect(op.getByRole('button', { name: 'Save changes' }).first()).toBeVisible();
      await expect(publicNamesGroup(op)).toHaveCount(0);
    });

    await test.step('a phone names every competitor', async () => {
      const spectator = await open(browser, PUBLIC_DEVICE);
      await openViewer(spectator, id);
      await expect(namesHiddenNote(spectator)).toHaveCount(0);
      await competitionTab(spectator, 'Pools').tap();
      await expect(spectator.locator('.pool-match-numbered-row')).toHaveCount(6);
      expect(await namesOnPage(spectator, JUNIOR_NAMES)).toEqual(JUNIOR_NAMES);
      await shot(spectator, 'self-run-pools');
    });
  });
});
