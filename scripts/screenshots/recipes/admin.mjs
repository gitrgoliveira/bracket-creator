// Admin console captures riding the seeded demo tournament.
import { loginAdmin } from '../lib/ui.mjs';
import { demoTournament } from '../lib/seed.mjs';

export const families = {
  // The tournament the committed captures were taken against: "London Cup
  // Demo", six competitions. Seeded by the same script `make
  // mobile-app-example` runs, so the docs and the demo cannot drift apart.
  demo: {
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
