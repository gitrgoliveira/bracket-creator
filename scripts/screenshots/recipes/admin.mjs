// Admin console captures riding the seeded demo tournament.
import { demoTournament } from '../lib/seed.mjs';
import { VIEWER_SOURCES } from '../lib/scope.mjs';

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
];
