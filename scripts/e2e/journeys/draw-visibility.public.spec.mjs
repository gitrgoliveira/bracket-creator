// A draft's draw kept off the public pages until it is published, on the
// PUBLIC project (a 390x844 phone, no sign-in).
//
// A generated draw starts hidden: a spectator's phone reads the competition as
// one still being prepared (no Pools or Bracket tab, no competitor number, a
// "Draw being prepared" card and badge), and the court's TV board shows none of
// its matches. The organiser taps Publish draw on the draw preview header and
// the phone shows the draw without a reload; Unpublish takes it away again,
// a phone on the Pools tab landing back on the Overview. Starting a hidden
// draw asks first, and the start makes it public.
//
// The test's own `page` is the spectator's phone, a fresh one per test. The
// tournament is seeded from a SECOND context with the operator's iPad
// (OPERATOR_DEVICE), through the interface, and that page is kept for the
// whole describe.
//
// One individual competition:
//   D  Draw Open  6 competitors, two pools of 3, court A, starting 10:30
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { createTournament } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import {
  DRAW_VISIBILITY, drawVisibilityHint, generateDraw, pasteRoster, publishDraw, startCompetition, unpublishDraw,
} from '../fixtures/competition.mjs';
import { toastText } from '../fixtures/roster.mjs';
import { competitionTab, openTvBoard, openViewer, tvBoard } from '../fixtures/public.mjs';
import { escape } from '../fixtures/text.mjs';

const COMP_NAME = 'Draw Open';
const START_TIME = '10:30';
const ROSTER = [
  ['Akira Tanaka', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Chie Mori', 'Musashi'],
  ['Dan Evans', 'Seishinkan'], ['Eri Kato', 'Hizen'], ['Finn Hale', 'Mumeishi'],
];
const NAMES = ROSTER.map(([n]) => n);

// What the public pages call a hidden draft (web-mobile/js/draw_visibility.jsx).
const PREPARING = 'Draw being prepared';
const PREPARING_MESSAGE = `Starts at ${START_TIME}. ${ROSTER.length} competitors entered. The draw shows here once it is published.`;
// The dashboard card's line under the name.
const CARD_LINE = { hidden: 'Hidden from the public', public: 'Public' };

const preparingCard = (page) => page.getByTestId('draw-preparing-card');
const drawTabs = (page) => page.locator('.viewer__tab', { hasText: /^(Pools|Bracket)$/ });
const poolRows = (page) => page.locator('.pool-match-numbered-row');
// The competitor numbers from the draw under `root`: an element whose whole
// text is the prefix and a number, e.g. D3. (The page text runs a number into
// the name beside it, "1D1Akira", so no word boundary finds one there.)
const drawNumbers = (root) => root.getByText(/^D\d+$/);
const homeRow = (page) => page.locator('.vlist-item--comp', { hasText: COMP_NAME });
const cardLine = (op) => op.locator('.tcard', { hasText: COMP_NAME }).getByTestId('card-draw-visibility');

// Which roster names the page's text holds.
async function namesOnPage(page) {
  const text = await page.locator('body').innerText();
  return NAMES.filter((n) => text.includes(n));
}

// The phone reads the competition as being prepared: the card on the
// Overview, no tab that would show the draw, and no number from it.
async function expectPreparing(page) {
  await expect(preparingCard(page)).toBeVisible();
  await expect(preparingCard(page)).toContainText(PREPARING);
  await expect(preparingCard(page)).toContainText(PREPARING_MESSAGE);
  await expect(drawTabs(page)).toHaveCount(0);
  await expect(drawNumbers(page.locator('.viewer__body'))).toHaveCount(0);
}

test.describe.configure({ mode: 'serial' });

test.describe('a draft draw kept off the public pages until it is published', () => {
  let shot;
  let opCtx;
  let op; // the operator's page, signed in, kept for the whole describe
  let id;

  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    shot = journeyShots('draw-visibility');
    const server = await serverForFile(testInfo.file);
    opCtx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: server.base });
    op = await opCtx.newPage();
    await createTournament(op, { name: 'Draw Cup', courts: 2 });
    id = await createCompetition(op, {
      name: COMP_NAME, kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A'],
      numberPrefix: 'D', startTime: START_TIME,
    });
    await pasteRoster(op, id, ROSTER);
    await generateDraw(op, id);
  });

  test.afterAll(async () => {
    await opCtx?.close();
  });

  test('a generated draw is hidden: the phone reads the competition as being prepared', async ({ page }) => {
    await test.step('the draw preview header says the public cannot see it', async () => {
      await expect(drawVisibilityHint(op)).toHaveText(DRAW_VISIBILITY.hiddenHint);
      await expect(op.getByTestId('draw-visibility-toggle')).toHaveText(DRAW_VISIBILITY.publish);
      await shot(op, 'header-hidden');
    });

    await test.step('the dashboard card says "Hidden from the public"', async () => {
      await op.goto('/admin');
      await expect(cardLine(op)).toHaveText(CARD_LINE.hidden);
      // The organiser's own badge keeps "Draw ready".
      await expect(op.locator('.tcard', { hasText: COMP_NAME }).locator('.badge--draw-ready')).toBeVisible();
    });

    await test.step('the public home: the badge reads "Draw being prepared"', async () => {
      await page.goto('/');
      await expect(page.locator('.viewer__title')).toHaveText('Draw Cup');
      await expect(homeRow(page).locator('.badge--draw-preparing')).toHaveText(PREPARING);
      await expect(homeRow(page).locator('.badge--draw-ready')).toHaveCount(0);
    });

    await test.step('the competition page: the "Draw being prepared" card, no Pools or Bracket tab, no number', async () => {
      await openViewer(page, id);
      await expectPreparing(page);
      await expect(page.locator('.viewer__head .badge--draw-preparing')).toHaveText(PREPARING);
      await shot(page, 'public-preparing', { fullPage: true });
    });

    await test.step('the TV board of court A shows none of its matches', async () => {
      await openTvBoard(page, 'A');
      expect(await namesOnPage(page), 'no competitor of the hidden draw on the TV board').toEqual([]);
      await expect(drawNumbers(tvBoard(page))).toHaveCount(0);
    });
  });

  test('Publish draw shows the draw on an open phone without a reload; Unpublish takes it away again', async ({ page }) => {
    await openViewer(page, id);
    await expectPreparing(page);

    await test.step('the organiser taps Publish draw: the header and the toast say it is public', async () => {
      await publishDraw(op, id);
      expect(await toastText(op, new RegExp(`^${escape(DRAW_VISIBILITY.publishedToast)}$`)))
        .toBe(DRAW_VISIBILITY.publishedToast);
      await shot(op, 'header-published');
    });

    await test.step('the open phone shows the Pools tab and the draw, with no reload', async () => {
      await expect(preparingCard(page)).toHaveCount(0);
      await expect(competitionTab(page, 'Pools')).toBeVisible();
      await competitionTab(page, 'Pools').tap();
      // Two pools of three: three matches each.
      await expect(poolRows(page)).toHaveCount(6);
      expect(await namesOnPage(page)).toEqual(NAMES);
      await expect(drawNumbers(page.locator('.viewer__body')).first()).toBeVisible();
      await shot(page, 'public-pools-published');
    });

    await test.step('the dashboard card says "Public"', async () => {
      await op.goto('/admin');
      await expect(cardLine(op)).toHaveText(CARD_LINE.public);
    });

    await test.step('the organiser taps Unpublish: the phone on the Pools tab lands back on the Overview', async () => {
      await unpublishDraw(op, id);
      expect(await toastText(op, new RegExp(`^${escape(DRAW_VISIBILITY.unpublishedToast)}$`)))
        .toBe(DRAW_VISIBILITY.unpublishedToast);
      await expectPreparing(page);
      await expect(poolRows(page)).toHaveCount(0);
      await shot(page, 'public-unpublished');
    });

    await test.step('a fresh load of the phone still reads it as being prepared', async () => {
      await page.reload();
      await expect(page.locator('.viewer__body')).toBeVisible();
      await expectPreparing(page);
    });
  });

  test('starting a hidden draw asks first, and the start makes the draw public', async ({ page }) => {
    await openViewer(page, id);
    await expectPreparing(page);

    await test.step('Start competition asks about the hidden draw, and Start competition in the confirm starts it', async () => {
      await op.goto(`/admin/competition/${id}`);
      await expect(drawVisibilityHint(op)).toHaveText(DRAW_VISIBILITY.hiddenHint);
      const asked = await startCompetition(op, id);
      expect(asked, 'the start of a hidden draw asks first').toBe(DRAW_VISIBILITY.startConfirm(COMP_NAME));
    });

    await test.step('the open phone shows the draw, with no reload', async () => {
      await expect(preparingCard(page)).toHaveCount(0);
      await expect(competitionTab(page, 'Pools')).toBeVisible();
      await competitionTab(page, 'Pools').tap();
      await expect(poolRows(page)).toHaveCount(6);
      expect(await namesOnPage(page)).toEqual(NAMES);
      await expect(page.locator('.viewer__head .badge--draw-preparing')).toHaveCount(0);
    });
  });
});
