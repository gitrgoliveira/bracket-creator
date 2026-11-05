// Admin console captures riding the seeded demo tournament.
import { demoTournament } from '../lib/seed.mjs';
import { VIEWER_SOURCES } from '../lib/scope.mjs';

// The competition calendar's fixture: a two-day tournament on shiaijo A and B
// with an opening ceremony and a lunch every day, one drawn competition (its
// times come from the draw), one in setup that carries an expected number (an
// estimate), and a kachinuki competition on the second day. Built over the
// HTTP API, which is allowed here because the calendar shows placed blocks,
// not a bout or a point that an HTTP write could silently drop.
const CAL_DATE_1 = '16-05-2026';
const CAL_DATE_2 = '17-05-2026';
const CAL_DRAWN = "Men's Individual";
const CAL_SETUP = "Women's Individual";
const CAL_PAUSES = [
  { name: 'Opening ceremony', start: '09:00', length: '30m', day: 0 },
  { name: 'Lunch', start: '12:30', length: '1h', day: 0 },
];
const CAL_ROSTER = [
  ['Haruki Tanaka', 'Kenshinkan'], ['Ren Suzuki', 'Musashi Dojo'], ['Sota Yamamoto', 'Seishinkan'],
  ['Yuto Watanabe', 'Kenshinkan'], ['Riku Nakamura', 'Musashi Dojo'], ['Kaito Kobayashi', 'Seibukan'],
  ['Daiki Yoshida', 'Hokushinkan'], ['Hinata Kato', 'Kenshinkan'], ['Sora Sasaki', 'Musashi Dojo'],
  ['Takumi Ito', 'Hokushinkan'], ['Yuma Saito', 'Seibukan'], ['Aoi Takahashi', 'Hokushinkan'],
].map(([name, dojo]) => ({ name, dojo }));

async function seedCalendar({ api }) {
  await api.tournament({
    name: 'Spring Kendo Taikai 2026', date: CAL_DATE_1, durationDays: 2, courts: ['A', 'B'], pauses: CAL_PAUSES,
  });
  await api.competition('cal-mens', CAL_DRAWN, {
    date: CAL_DATE_1, courts: ['A', 'B'], startTime: '09:30',
  });
  await api.participants('cal-mens', CAL_ROSTER);
  await api.generateDraw('cal-mens');
  await api.competition('cal-womens', CAL_SETUP, {
    date: CAL_DATE_1, courts: ['A', 'B'], startTime: '14:00', expectedParticipants: 16,
  });
  await api.competition('cal-kachinuki', 'Kachinuki Teams', {
    date: CAL_DATE_2, courts: ['A'], startTime: '09:30', format: 'knockout',
    teamSize: 5, teamMatchType: 'kachinuki', expectedParticipants: 8,
  });
}

// The Move dialog of the setup competition, opened the way the page documents
// it: focus the block, press M. Dragging is not used because a recipe cannot
// show a drag in progress as a still, and the dialog is the same state a drag
// ends in.
async function openMoveDialog(page, name) {
  const block = page.getByRole('button', { name: new RegExp(`^${name},`) });
  await block.waitFor({ state: 'visible', timeout: 15000 });
  await block.focus();
  await page.keyboard.press('M');
  await page.getByTestId('cal-move-dialog').waitFor({ state: 'visible' });
  await page.getByLabel('Start time').waitFor({ state: 'visible' });
}

async function setMoveStart(page, value) {
  await page.getByLabel('Start time').fill(value);
  // The preview behind the dialog is debounced; its summary line carries
  // aria-live and appears once the new length is worked out.
  await page.locator('.cal-move-dialog [aria-live="polite"]').first().waitFor({ state: 'visible', timeout: 15000 });
}

export const families = {
  // The tournament the committed captures were taken against: "London Cup
  // Demo", six competitions. Seeded by the same script `make
  // mobile-app-example` runs, so the docs and the demo cannot drift apart.
  demo: {
    server: 'mobile',
    // What a SINCE-scoped run treats as this family's inputs (lib/scope.mjs):
    // the demo seed script and its CSVs, and the public viewer pages two of its
    // captures show. Shared modules are claimed by no family on purpose - a
    // change there runs everything. That includes admin.jsx, which routes every
    // admin page, and admin_shell.jsx, the dashboard's host and every admin
    // page's frame, although this family's dashboard capture shows them.
    sources: [
      'scripts/setup_tournament.py', 'test-data/', ...VIEWER_SOURCES,
    ],
    seed: async ({ base }) => {
      await demoTournament(base);
      return {};
    },
  },

  // The competition calendar and a move. One family per state, because a
  // recipe that confirms a move changes the fixture for the next one.
  calendar: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the calendar page, its move dialog
    // and rules, and the pause editor whose bands it draws.
    sources: ['web-mobile/js/admin_calendar', 'web-mobile/js/calendar_', 'web-mobile/js/pause_list'],
    seed: seedCalendar,
  },
  calendar_moved: {
    server: 'mobile',
    sources: ['web-mobile/js/admin_calendar', 'web-mobile/js/calendar_', 'web-mobile/js/pause_list'],
    seed: seedCalendar,
  },
};

export const recipes = [
  {
    name: 'mobile-dashboard',
    family: 'demo',
    route: '/admin',
    viewport: { width: 1280, height: 900 },
    dpr: 2,
    capture: 'viewport',
    auth: 'admin',
    waitFor: 'text=Admin console',
  },

  {
    name: 'mobile-calendar',
    family: 'calendar',
    route: '/admin/calendar',
    viewport: { width: 1280, height: 900 },
    capture: 'viewport',
    auth: 'admin',
    waitFor: `text=${CAL_DRAWN}`,
  },

  {
    // The Move dialog on a competition that is not drawn yet, moved to 11:00 so
    // it lands on the drawn competition and the dialog warns about the overlap
    // without refusing it.
    name: 'mobile-calendar-move',
    family: 'calendar',
    route: '/admin/calendar',
    viewport: { width: 1280, height: 900 },
    capture: 'viewport',
    auth: 'admin',
    waitFor: `text=${CAL_SETUP}`,
    drive: async ({ page }) => {
      await openMoveDialog(page, CAL_SETUP);
      await setMoveStart(page, '11:00');
    },
  },

  {
    // The calendar straight after a move is confirmed: the block in its new
    // place, and the "Moved just now" line with its Undo button above it.
    name: 'mobile-calendar-moved',
    family: 'calendar_moved',
    route: '/admin/calendar',
    viewport: { width: 1280, height: 900 },
    capture: 'viewport',
    auth: 'admin',
    waitFor: `text=${CAL_SETUP}`,
    drive: async ({ page }) => {
      await openMoveDialog(page, CAL_SETUP);
      await setMoveStart(page, '11:00');
      await page.getByTestId('cal-move-dialog').getByRole('button', { name: 'Move', exact: true }).click();
      await page.getByTestId('cal-moved-line').waitFor({ state: 'visible', timeout: 15000 });
    },
  },
];
