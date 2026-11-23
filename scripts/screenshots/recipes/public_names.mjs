// Competitor names on the public pages: the organiser's setting, what a
// spectator sees while names are hidden, and what the same page shows after a
// reveal link.
//
// One officiated tournament with one junior competition set to "Numbers, one
// competition link". Everything here is scaffolding in api.mjs's sense: the
// roster, the draw and the setting are made over the same calls the Add
// competition form makes, and no member number, bout row or point is shown. The
// pools are still to be fought, so the shots carry numbers and no result.
//
// The captures:
//   public-names-setting         the competition's settings, with the option,
//                                the expiry and the competition link
//   public-names-tournament      the tournament settings' default for new
//                                competitions
//   public-names-viewer-masked   the competition page in a private window:
//                                competitor numbers and dojos, no names
//   public-names-viewer-revealed the same page after the reveal link, names
//                                shown
//
// The reveal key is minted when the competition is saved, so it differs on every
// run: the seed reads it back from the organiser's own endpoint (the one the
// settings page reads) and the revealed capture opens the link the QR code
// carries. Both viewer captures run in a context that never signed in, and each
// recipe has a context of its own, so the key one capture stores never reaches
// the masked one.
import { VIEWER_SOURCES } from '../lib/scope.mjs';

const COMP = 'junior-individual';
const TOURNAMENT_NAME = 'Northgate Junior Taikai';
// A reveal link stops working at the end of the tournament, so the date is far
// enough ahead that the link is still open whenever the harness is run. The
// capture clock is not faked: the settings page reads "now" from the server.
const TOURNAMENT_DATE = '15-05-2037';

// Real names no spectator should see. The masked capture is refused if any of
// them is on the page, which is the whole point of the shot.
const ROSTER = [
  ['Daniel Brooks', 'Northgate Kendo Club'], ['Sota Yamamoto', 'Seishinkan'],
  ['Felix Bauer', 'Berlin Kenyu'], ['Riku Nakamura', 'Musashi Dojo'],
  ['Pierre Dubois', 'Paris Kendo Club'], ['Erik Lund', 'Nordic Kendo'],
].map(([name, dojo]) => ({ name, dojo }));
const NAMES = ROSTER.map((p) => p.name);

// The labels a person reads (public_names_fields.jsx).
const LABEL_PUBLIC_NAMES = 'Competitor names on public pages';

export const families = {
  publicNames: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the option's controls and words, the
    // competition settings and tournament settings that hold them, the reveal
    // link page and the stored keys, and the viewer pages the captures open.
    sources: [
      ...VIEWER_SOURCES,
      'web-mobile/js/public_names_fields', 'web-mobile/js/reveal_key', 'web-mobile/js/viewer_reveal',
      'web-mobile/js/admin_competition_settings', 'web-mobile/js/admin_setup',
    ],
    seed: async ({ api }) => {
      // The Public URL is what lets the settings page print the competition's QR
      // code (it is built from that address), so it is set here as an organiser
      // would for a real event.
      await api.tournament({
        name: TOURNAMENT_NAME,
        date: TOURNAMENT_DATE,
        venue: 'Northgate Sports Hall',
        courts: ['A', 'B'],
        publicURL: 'https://taikai.example.org',
      });
      await api.competition(COMP, 'Junior Individual', {
        date: TOURNAMENT_DATE,
        courts: ['A'],
        numberPrefix: 'J',
        publicNames: 'competition',
      });
      await api.participants(COMP, ROSTER);
      await api.generateDraw(COMP);
      await api.start(COMP);

      // The competition link's key, as the settings page reads it.
      const links = await api.get(`/api/competitions/${COMP}/reveal-links`);
      if (!links.competitionKey) throw new Error(`${COMP} has no competition reveal key: the setting did not take`);
      if (!links.open) throw new Error(`${COMP}'s reveal link is closed, so the revealed capture would show no names`);
      return { compId: COMP, key: links.competitionKey };
    },
  },
};

// Put the group named `label` near the top of the viewport, with the controls
// that follow it in frame. A scroll rather than a crop, so the shot is the page
// as it is.
async function showGroup(page, label) {
  const group = page.getByRole('group', { name: label, exact: true });
  await group.waitFor({ state: 'visible', timeout: 20000 });
  await group.evaluate((el) => {
    const top = el.getBoundingClientRect().top + window.scrollY;
    window.scrollTo(0, Math.max(0, top - 160));
  });
  await page.waitForTimeout(350);
}

const bodyText = (page) => page.locator('body').innerText();

export const recipes = [
  {
    // The competition's settings: the picker, the expiry and the competition
    // link with its Copy and QR buttons.
    name: 'public-names-setting',
    family: 'publicNames',
    route: `/admin/competition/${COMP}/settings`,
    auth: 'admin',
    viewport: { width: 860, height: 1100 },
    waitFor: '[data-testid="reveal-links-panel"]',
    drive: async ({ page }) => {
      // The link is read from the server a moment after the panel draws.
      await page.getByRole('button', { name: 'Copy link', exact: true }).waitFor({ state: 'visible', timeout: 20000 });
      await showGroup(page, LABEL_PUBLIC_NAMES);
    },
  },
  {
    // The tournament's default: what new competitions start with.
    name: 'public-names-tournament',
    family: 'publicNames',
    route: '/admin/edit-tournament',
    auth: 'admin',
    viewport: { width: 860, height: 900 },
    drive: async ({ page }) => {
      await showGroup(page, LABEL_PUBLIC_NAMES);
    },
  },
  {
    // A spectator's phone before any link: numbers and dojos, never a name.
    name: 'public-names-viewer-masked',
    family: 'publicNames',
    route: `/competition/${COMP}`,
    viewport: { width: 430, height: 560 },
    dpr: 2,
    waitFor: '.viewer__tab',
    assert: async ({ page }) => {
      const text = await bodyText(page);
      const leaked = NAMES.filter((n) => text.includes(n));
      if (leaked.length) throw new Error(`the masked page shows ${leaked.join(', ')}: the capture would show names`);
      if (!/\bJ\d+\b/.test(text)) throw new Error('the masked page shows no competitor number (J1, J2, ...): the draw never numbered anyone');
    },
  },
  {
    // The same page after the competition link: a spectator who scanned the
    // venue's QR code sees every name.
    name: 'public-names-viewer-revealed',
    family: 'publicNames',
    viewport: { width: 430, height: 560 },
    dpr: 2,
    drive: async ({ page, base, fixture }) => {
      await page.goto(`${base}/reveal/${fixture.compId}#${fixture.key}`, { waitUntil: 'domcontentloaded' });
      // The link page offers the competition when it does not open it itself.
      const tab = page.locator('.viewer__tab').first();
      const open = page.getByRole('button', { name: 'Open the competition', exact: true });
      await tab.or(open).first().waitFor({ state: 'visible', timeout: 20000 });
      if (await open.isVisible()) await open.click();
      await tab.waitFor({ state: 'visible', timeout: 20000 });
    },
    assert: async ({ page }) => {
      const text = await bodyText(page);
      if (!NAMES.some((n) => text.includes(n))) {
        throw new Error('the revealed page shows no competitor name: the link revealed nothing');
      }
    },
  },
];
