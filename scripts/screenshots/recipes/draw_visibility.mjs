// A draft's draw kept off the public pages until it is published: the draw
// preview header in both states, the dashboard card's line, and what a
// spectator sees while the draw is hidden.
//
// One officiated tournament with one individual competition whose draw is
// generated and not started. Everything here is scaffolding in api.mjs's
// sense: the roster, the draw and its visibility are the calls the Add
// competition form and the draw preview header make, and no member number,
// bout row or point is shown.
//
// The captures:
//   draw-hidden-header       the draw preview header of a hidden draw: Discard
//                            draw, Publish draw, Start competition, and the hint
//                            line saying the public cannot see it
//   draw-published-header    the same header once published: Unpublish, and the
//                            hint line saying changes show at once
//   draw-hidden-card         the dashboard card's line, "Hidden from the public"
//   draw-preparing-public    a spectator's phone on the competition while the
//                            draw is hidden: the "Draw being prepared" card
//
// Each capture sets the visibility it photographs over the API before it opens
// its page (publish and unpublish answer 200 when the draw is already in that
// state), so a capture run alone (NAME=) shows the same picture as a run of the
// whole family, whatever order the runner takes them in.
import { VIEWER_SOURCES } from '../lib/scope.mjs';

const COMP = 'senior-individual';
const COMP_NAME = 'Senior Individual';
const TOURNAMENT_NAME = 'Riverside Kendo Taikai';
const TOURNAMENT_DATE = '17-10-2026';
const START_TIME = '10:30';

const ROSTER = [
  ['Thomas Reed', 'Riverside Kendo Club'], ['Haruto Sato', 'Seishinkan'],
  ['Lukas Weber', 'Berlin Kenyu'], ['Kenji Ito', 'Musashi Dojo'],
  ['Antoine Martin', 'Paris Kendo Club'], ['Oskar Berg', 'Nordic Kendo'],
  ['Marco Rossi', 'Milano Kendo'], ['Yuki Hayashi', 'Hizen Dojo'],
].map(([name, dojo]) => ({ name, dojo }));

// The words a person reads (web-mobile/js/draw_visibility.jsx).
const TEXT = {
  hiddenHint: 'Hidden from the public. Publish draw or Start competition makes it public.',
  publishedHint: 'Published. The public can see this draw. Changes you make show on public pages at once.',
  hiddenCardLine: 'Hidden from the public',
  preparing: 'Draw being prepared',
  preparingMessage: `Starts at ${START_TIME}. ${ROSTER.length} competitors entered. The draw shows here once it is published.`,
};

export const families = {
  drawVisibility: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the one owner of the visibility's
    // rule and words, the draw preview header, the dashboard card, the status
    // badge, and the viewer pages the public capture opens.
    sources: [
      ...VIEWER_SOURCES,
      'web-mobile/js/draw_visibility', 'web-mobile/js/admin_competition', 'web-mobile/js/admin_shell',
      'web-mobile/js/ui', 'web-mobile/js/icon',
    ],
    seed: async ({ api }) => {
      await api.tournament({
        name: TOURNAMENT_NAME, date: TOURNAMENT_DATE, venue: 'Riverside Sports Centre', courts: ['A', 'B'],
      });
      await api.competition(COMP, COMP_NAME, {
        date: TOURNAMENT_DATE, courts: ['A', 'B'], numberPrefix: 'S', startTime: START_TIME,
      });
      await api.participants(COMP, ROSTER);
      await api.generateDraw(COMP);
      return { compId: COMP };
    },
  },
};

const visible = (page, selector) => page.locator(selector).first().waitFor({ state: 'visible', timeout: 20000 });

// Open the competition's draw preview with its draw hidden or published.
async function openHeader({ page, base, api }, hidden) {
  await (hidden ? api.unpublishDraw(COMP) : api.publishDraw(COMP));
  await page.goto(`${base}/admin/competition/${COMP}`, { waitUntil: 'domcontentloaded' });
  await visible(page, `.draw-visibility-hint--${hidden ? 'hidden' : 'published'}`);
}

async function hintIs(page, want) {
  const got = (await page.getByTestId('draw-visibility-hint').innerText()).trim();
  if (got !== want) throw new Error(`the hint line reads "${got}", not "${want}"`);
}

export const recipes = [
  {
    // The header of a freshly generated draw: hidden from the public.
    name: 'draw-hidden-header',
    family: 'drawVisibility',
    auth: 'admin',
    viewport: { width: 1160, height: 700 },
    dpr: 2,
    capture: { selector: '.page-head' },
    drive: (args) => openHeader(args, true),
    assert: async ({ page }) => {
      await hintIs(page, TEXT.hiddenHint);
      const label = (await page.getByTestId('draw-visibility-toggle').innerText()).trim();
      if (label !== 'Publish draw') throw new Error(`the toggle reads "${label}", not "Publish draw"`);
    },
  },
  {
    // The same header after Publish draw.
    name: 'draw-published-header',
    family: 'drawVisibility',
    auth: 'admin',
    viewport: { width: 1160, height: 700 },
    dpr: 2,
    capture: { selector: '.page-head' },
    drive: (args) => openHeader(args, false),
    assert: async ({ page }) => {
      await hintIs(page, TEXT.publishedHint);
      const label = (await page.getByTestId('draw-visibility-toggle').innerText()).trim();
      if (label !== 'Unpublish') throw new Error(`the toggle reads "${label}", not "Unpublish"`);
    },
  },
  {
    // The dashboard card of the hidden draw: its line under the name.
    name: 'draw-hidden-card',
    family: 'drawVisibility',
    auth: 'admin',
    viewport: { width: 1280, height: 900 },
    dpr: 2,
    capture: { selector: '.tcard:has([data-testid="card-draw-visibility"])' },
    drive: async ({ page, base, api }) => {
      await api.unpublishDraw(COMP);
      await page.goto(`${base}/admin`, { waitUntil: 'domcontentloaded' });
      await visible(page, '[data-testid="card-draw-visibility"]');
    },
    assert: async ({ page }) => {
      const got = (await page.getByTestId('card-draw-visibility').first().innerText()).trim();
      if (got !== TEXT.hiddenCardLine) throw new Error(`the card's line reads "${got}", not "${TEXT.hiddenCardLine}"`);
    },
  },
  {
    // A spectator's phone, never signed in, while the draw is hidden: the
    // Overview's card, and no Pools or Bracket tab to open.
    name: 'draw-preparing-public',
    family: 'drawVisibility',
    viewport: { width: 430, height: 560 },
    dpr: 2,
    drive: async ({ page, base, api }) => {
      await api.unpublishDraw(COMP);
      await page.goto(`${base}/competition/${COMP}`, { waitUntil: 'domcontentloaded' });
      await visible(page, '[data-testid="draw-preparing-card"]');
    },
    assert: async ({ page }) => {
      const card = (await page.getByTestId('draw-preparing-card').innerText()).replace(/\s+/g, ' ');
      for (const want of [TEXT.preparing, TEXT.preparingMessage]) {
        if (!card.includes(want)) throw new Error(`the public card does not say "${want}" (it reads "${card}")`);
      }
      const tabs = await page.locator('.viewer__tab').allInnerTexts();
      const drawTabs = tabs.map((t) => t.trim()).filter((t) => /^(Pools|Bracket)$/.test(t));
      if (drawTabs.length) throw new Error(`the hidden draw still offers ${drawTabs.join(' and ')} to the public`);
      const text = await page.locator('body').innerText();
      if (/\bS\d+\b/.test(text)) throw new Error('the public page shows a competitor number from the hidden draw');
    },
  },
];
