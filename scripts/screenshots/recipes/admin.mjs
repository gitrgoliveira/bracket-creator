// Admin console captures riding the seeded demo tournament.
import { loginAdmin } from '../lib/ui.mjs';
import { demoTournament } from '../lib/seed.mjs';

export const families = {
  // The tournament the committed captures were taken against: "London Cup
  // Demo", six competitions. Seeded by the same script `make
  // mobile-app-example` runs, so the docs and the demo cannot drift apart.
  demo: {
    // What a SINCE-scoped run treats as this family's inputs (lib/scope.mjs):
    // the demo seed script and its CSVs, the admin dashboard, and the public
    // viewer pages two of its captures show. Shared modules are claimed by no
    // family on purpose - a change there runs everything.
    sources: [
      'scripts/setup_tournament.py', 'test-data/',
      'web-mobile/js/admin.jsx', 'web-mobile/js/viewer',
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
    server: 'mobile',
    family: 'demo',
    route: '/admin',
    viewport: { width: 1280, height: 900 },
    dpr: 2,
    capture: 'viewport',
    setup: ({ page, base }) => loginAdmin(page, base),
    waitFor: 'text=Admin console',
  },
];
