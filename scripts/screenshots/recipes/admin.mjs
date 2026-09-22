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
    // the demo seed script and its CSVs, the admin dashboard, and the public
    // viewer pages two of its captures show. Shared modules are claimed by no
    // family on purpose - a change there runs everything.
    sources: [
      'scripts/setup_tournament.py', 'test-data/',
      'web-mobile/js/admin.jsx', ...VIEWER_SOURCES,
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
