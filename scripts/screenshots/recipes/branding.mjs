// A branded tournament: the public viewer home with a logo, an accent colour
// and a sponsor image set, for the landing page's "Your branding" card.
//
// Everything here is the organiser's setup, made over the same calls the Edit
// details page makes (the logo and sponsor uploads, then the tournament save
// with its theme), so it is scaffolding in api.mjs's sense: no member number,
// bout row or point is involved. The two images are drawn here from SVG in
// the harness's own browser and uploaded as PNG (the upload doors take PNG or
// JPEG only), so no binary sits in the repository and the club and sponsor
// are as made up as the rosters.
import { PASSWORD } from '../lib/api.mjs';
import { VIEWER_SOURCES } from '../lib/scope.mjs';

const TOURNAMENT_NAME = 'Northgate Taikai 2026';
const TOURNAMENT_DATE = '20-06-2026';

// A deep green the default navy theme never uses, so the card shows at a
// glance that the colours are the organiser's, with its soft tint.
const THEME = { primaryColor: '#1f6f4a', accentSoftColor: '#e2f0e8' };

const LOGO_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="240" height="240" viewBox="0 0 240 240">
  <circle cx="120" cy="120" r="114" fill="#1f6f4a"/>
  <circle cx="120" cy="120" r="98" fill="none" stroke="#fff" stroke-width="6"/>
  <path d="M120 44 L176 150 H64 Z" fill="#fff"/>
  <path d="M120 84 L150 142 H90 Z" fill="#1f6f4a"/>
  <text x="120" y="196" text-anchor="middle" font-family="Georgia, serif" font-size="30" font-weight="700" fill="#fff">NORTHGATE</text>
</svg>`;

const SPONSOR_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="240" viewBox="0 0 1200 240">
  <rect width="1200" height="240" rx="16" fill="#f6f1e7"/>
  <rect x="60" y="60" width="120" height="120" rx="12" fill="#7a4b24"/>
  <path d="M90 150 V96 Q120 70 150 96 V150 Z" fill="#f6f1e7"/>
  <text x="220" y="128" font-family="Georgia, serif" font-size="64" font-weight="700" fill="#3b2614">Hillside Bogu Works</text>
  <text x="222" y="178" font-family="Helvetica, Arial, sans-serif" font-size="32" fill="#7a4b24">Armour repairs and fittings at the venue all weekend</text>
</svg>`;

// Draw an SVG to a PNG in the harness's browser.
async function rasterise(browser, svg) {
  const context = await browser.newContext({ deviceScaleFactor: 1 });
  try {
    const page = await context.newPage();
    await page.setContent(`<!doctype html><body style="margin:0">${svg}</body>`);
    return await page.locator('svg').screenshot({ omitBackground: true });
  } finally {
    await context.close();
  }
}

// The two upload doors take a multipart form, which api.mjs's JSON client
// does not send.
async function upload(base, path, png, fields = {}) {
  const form = new FormData();
  for (const [k, v] of Object.entries(fields)) form.append(k, v);
  form.append('file', new Blob([png], { type: 'image/png' }), 'image.png');
  const res = await fetch(base + path, {
    method: 'POST', headers: { 'X-Tournament-Password': PASSWORD }, body: form,
  });
  if (!res.ok) throw new Error(`POST ${path} -> ${res.status} ${(await res.text()).slice(0, 200)}`);
}

export const families = {
  branding: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the viewer home it captures, the
    // sponsor strip and the branding fields whose saves the seed makes.
    sources: [...VIEWER_SOURCES, 'web-mobile/js/sponsor_strip', 'web-mobile/js/admin_branding'],
    seed: async ({ api, base, browser }) => {
      await api.tournament({ name: TOURNAMENT_NAME, date: TOURNAMENT_DATE, venue: 'Northgate Sports Hall', courts: ['A', 'B'] });

      await upload(base, '/api/branding/logo', await rasterise(browser, LOGO_SVG));
      await upload(base, '/api/sponsors', await rasterise(browser, SPONSOR_SVG), { name: 'Hillside Bogu Works' });

      // The save the Edit details page makes: the tournament as read, its
      // version, and the theme.
      const current = await api.get('/api/tournament');
      await api.put('/api/tournament', { ...current, password: '', theme: { ...(current.theme || {}), ...THEME } });

      await api.competition('mens-individual', "Men's Individual", { date: TOURNAMENT_DATE, courts: ['A'] });
      await api.participants('mens-individual', [
        { name: 'Daniel Brooks', dojo: 'Northgate Kendo Club' }, { name: 'Sota Yamamoto', dojo: 'Seishinkan' },
        { name: 'Felix Bauer', dojo: 'Berlin Kenyu' }, { name: 'Riku Nakamura', dojo: 'Musashi Dojo' },
        { name: 'Pierre Dubois', dojo: 'Paris Kendo Club' }, { name: 'Erik Lund', dojo: 'Nordic Kendo' },
      ]);
      await api.competition('womens-individual', "Women's Individual", { date: TOURNAMENT_DATE, courts: ['B'], startTime: '10:30' });
      await api.participants('womens-individual', [
        { name: 'Aiko Sato', dojo: 'Northgate Kendo Club' }, { name: 'Hana Ito', dojo: 'Seishinkan' },
        { name: 'Mio Abe', dojo: 'Musashi Dojo' }, { name: 'Yui Endo', dojo: 'Nordic Kendo' },
      ]);
    },
  },
};

export const recipes = [
  {
    // The public viewer home as a phone shows it: the logo in the header, the
    // accent colour on the page, and the sponsor image below the competitions.
    name: 'viewer-branded',
    family: 'branding',
    route: '/',
    viewport: { width: 430, height: 900 },
    dpr: 2,
    capture: 'fullPage',
    waitFor: '.viewer__title--lg',
  },
];
