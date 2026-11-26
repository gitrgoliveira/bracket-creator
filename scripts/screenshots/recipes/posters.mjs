// The Posters page: the printable QR-code posters for the venue, with the
// viewer-home poster (logo in the centre of the code) and a shiaijo poster
// (logo above the code).
//
// Everything here is the organiser's setup, made over the same calls the Edit
// details page and the Posters page make (the logo and sponsor uploads, then
// the tournament save with its theme and its poster choices), so it is
// scaffolding in api.mjs's sense: no member number, bout row or point is
// involved. The three images are drawn here from SVG in the harness's own
// browser and uploaded as PNG (the upload doors take PNG or JPEG only), so no
// binary sits in the repository and the club and sponsors are as made up as the
// rosters.
//
// The Public URL is set, as an organiser would for a real event: it is what the
// codes open, and without it the page shows its address warning instead of the
// plain page the guide describes.
//
// The captures:
//   posters-viewer-home   the page with the viewer-home poster in the preview
//   posters-court         the page with the Shiaijo A poster in the preview
import { VIEWER_SOURCES } from '../lib/scope.mjs';

const TOURNAMENT_NAME = 'Northgate Taikai 2026';
const TOURNAMENT_DATE = '20-06-2026';
const THEME = { primaryColor: '#1f6f4a', accentSoftColor: '#e2f0e8' };

const LOGO_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="240" height="240" viewBox="0 0 240 240">
  <circle cx="120" cy="120" r="114" fill="#1f6f4a"/>
  <circle cx="120" cy="120" r="98" fill="none" stroke="#fff" stroke-width="6"/>
  <path d="M120 44 L176 150 H64 Z" fill="#fff"/>
  <path d="M120 84 L150 142 H90 Z" fill="#1f6f4a"/>
  <text x="120" y="196" text-anchor="middle" font-family="Georgia, serif" font-size="30" font-weight="700" fill="#fff">NORTHGATE</text>
</svg>`;

const sponsorSvg = (title, colour) => `<svg xmlns="http://www.w3.org/2000/svg" width="480" height="240" viewBox="0 0 480 240">
  <rect width="480" height="240" rx="16" fill="#f6f1e7"/>
  <rect x="36" y="72" width="96" height="96" rx="12" fill="${colour}"/>
  <text x="152" y="132" font-family="Georgia, serif" font-size="34" font-weight="700" fill="#3b2614">${title}</text>
</svg>`;

// Draw an SVG to a PNG in the harness's browser, as the Blob an upload sends.
async function rasterise(browser, svg) {
  const context = await browser.newContext({ deviceScaleFactor: 1 });
  try {
    const page = await context.newPage();
    await page.setContent(`<!doctype html><body style="margin:0">${svg}</body>`);
    const png = await page.locator('svg').screenshot({ omitBackground: true });
    return new Blob([png], { type: 'image/png' });
  } finally {
    await context.close();
  }
}

export const families = {
  posters: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the page, the poster model and
    // layout, the print sheet and the encoder the code is drawn with.
    sources: [
      ...VIEWER_SOURCES,
      'web-mobile/js/admin_posters', 'web-mobile/js/poster_layout', 'web-mobile/js/posters_model',
      'web-mobile/js/qr', 'web-mobile/css/posters.css',
    ],
    seed: async ({ api, browser }) => {
      await api.tournament({
        name: TOURNAMENT_NAME,
        date: TOURNAMENT_DATE,
        venue: 'Northgate Sports Hall',
        courts: ['A', 'B'],
        publicURL: 'https://taikai.example.org',
      });

      await api.upload('/api/branding/logo', await rasterise(browser, LOGO_SVG));
      await api.upload('/api/sponsors', await rasterise(browser, sponsorSvg('Hillside Bogu', '#7a4b24')), { name: 'Hillside Bogu Works' });
      await api.upload('/api/sponsors', await rasterise(browser, sponsorSvg('Harbour Dojo', '#1d3557')), { name: 'Harbour Dojo Supplies' });

      // The save the Posters page makes: the tournament as read, its version,
      // the theme, and the poster choices.
      const current = await api.get('/api/tournament');
      await api.put('/api/tournament', {
        ...current,
        password: '',
        theme: { ...(current.theme || {}), ...THEME },
        posters: {
          viewer: { heading: 'Follow the Northgate Taikai live', callToAction: 'Scan for results, brackets and what is on next' },
          courts: { A: { logo: 'above', callToAction: 'Scan to follow this court' } },
        },
      });

      await api.competition('mens-individual', "Men's Individual", { date: TOURNAMENT_DATE, courts: ['A'] });
      await api.participants('mens-individual', [
        { name: 'Daniel Brooks', dojo: 'Northgate Kendo Club' }, { name: 'Sota Yamamoto', dojo: 'Seishinkan' },
        { name: 'Felix Bauer', dojo: 'Berlin Kenyu' }, { name: 'Riku Nakamura', dojo: 'Musashi Dojo' },
      ]);
    },
  },
};

// The preview is drawn a moment after the page; the Print buttons are the last
// thing on the choices card.
const READY = 'button:has-text("Print this poster")';

export const recipes = [
  {
    // The viewer-home poster, with the logo in the centre of its code.
    name: 'posters-viewer-home',
    family: 'posters',
    route: '/admin/posters',
    auth: 'admin',
    viewport: { width: 1100, height: 900 },
    capture: 'fullPage',
    waitFor: READY,
  },
  {
    // The Shiaijo A poster: the court letter, with the logo in the top corner.
    name: 'posters-court',
    family: 'posters',
    route: '/admin/posters',
    auth: 'admin',
    viewport: { width: 1100, height: 900 },
    capture: 'fullPage',
    waitFor: READY,
    drive: async ({ page }) => {
      await page.getByRole('button', { name: 'Shiaijo A', exact: true }).click();
      await page.getByRole('button', { name: 'Shiaijo A', exact: true, pressed: true }).waitFor({ state: 'visible', timeout: 20000 });
      await page.waitForTimeout(350);
    },
  },
];
