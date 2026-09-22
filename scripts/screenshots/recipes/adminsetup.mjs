// Admin SETUP surfaces: create-competition, participants/seeding, the
// pools-preview draw, and the kachinuki schedule-estimate range.
//
// Unlike the `demo` family (admin.mjs), these show a competition still in
// `setup` (or, for the pools-preview shot, `draw-ready`) status: nothing has
// been started or scored. So every fixture here is built straight over the
// HTTP API (tournament, competitions, participants, the draw) - allowed per
// api.mjs's own comment, since none of these captures show a member-number
// chip, a bout row, or a point that could be silently dropped by an HTTP
// write. Only the seed-rank assignment in mobile-participant-setup and the
// unapplied roster paste in mobile-add-participants are driven through the
// real interface, because those two are showing the OPERATOR TYPING, not
// just data that happens to be on screen.
import { loginAdmin } from '../lib/ui.mjs';

// Local helpers (not shared via lib/ui.mjs - see the bc-shot brief: only the
// recipe-owning file may change here). Each is small enough it wasn't worth
// promoting; if a second recipe file grows the same need, that's the signal
// to move it.

// The create-competition form and the settings screen both render this
// field via competition_fields.jsx's PillGroup/`.field` markup
// (web-mobile/js/admin_setup.jsx:1176-1227), so a field is found by its
// visible label rather than a CSS hook the markup doesn't expose one for.
async function fillFieldByLabel(page, label, value) {
  const field = page.locator('.field').filter({ has: page.locator('.field__label', { hasText: label }) }).first();
  await field.locator('input').first().fill(value);
}

// PillGroup renders each option as a `.radio-pill` button carrying its
// label verbatim (web-mobile/js/competition_fields.jsx:68-86), used for
// both the Format and Competition-type groups on the create form.
async function clickPill(page, label) {
  await page.locator('.radio-pill', { hasText: label }).first().click();
}

// Seed-rank input: aria-label is `Seed rank for ${p.name}`
// (web-mobile/js/admin_participants.jsx:972). StableInput (ui.jsx:149)
// commits on blur immediately (no need to wait out its 200ms debounce), so
// a Tab press after fill is enough to land the PUT.
async function setSeedRank(page, name, rank) {
  const input = page.locator(`input[aria-label="Seed rank for ${name}"]`);
  await input.waitFor({ state: 'visible', timeout: 15000 });
  await input.fill(String(rank));
  await input.press('Tab');
  await page.waitForTimeout(400);
}

const TOURNAMENT_NAME = 'Spring Kendo Taikai 2026';
const TOURNAMENT_DATE = '16-05-2026';

// The 18-name/dojo roster that mobile-draw-preview's committed shot draws
// into 6 pools of 3 (default poolSize). Reused for mobile-participants too,
// so both captures show a plausible, internally-consistent roster.
const ROSTER_18 = [
  ['Daniel Brooks', 'Eikoku Kendo Club'], ['William Hill', 'Thames Kendo Kai'], ['Felix Bauer', 'Berlin Kenyu'],
  ['Sota Yamamoto', 'Seishinkan'], ['Riku Nakamura', 'Musashi Dojo'], ['Erik Lund', 'Nordic Kendo'],
  ['Haruki Tanaka', 'Kenshinkan'], ['Pierre Dubois', 'Paris Kendo Club'], ['Luca Conti', 'Milano Kendo'],
  ['Ren Suzuki', 'Musashi Dojo'], ['Hinata Kato', 'Seishinkan'], ['Antoine Moreau', 'Paris Kendo Club'],
  ['Yuto Watanabe', 'Kenshinkan'], ['Marco Rossi', 'Milano Kendo'], ['Tomas Novak', 'Praha Kendo'],
  ['Oliver Walker', 'Thames Kendo Kai'], ['Jonas Weber', 'Berlin Kenyu'], ['Mikael Berg', 'Nordic Kendo'],
].map(([name, dojo]) => ({ name, dojo }));

const ROSTER_10 = [
  ['Aiko Sato', 'Tokyo'], ['Kenji Mori', 'Osaka'], ['Yuki Sato', 'Kyoto'], ['Hana Ito', 'Tokyo'],
  ['Ren Kato', 'Osaka'], ['Mio Abe', 'Kyoto'], ['Sora Kimura', 'Tokyo'], ['Taro Ono', 'Osaka'],
  ['Yui Endo', 'Kyoto'], ['Sho Fujii', 'Tokyo'],
].map(([name, dojo]) => ({ name, dojo }));

export const families = {
  // Covers every recipe except kachinuki-estimate-range: one tournament,
  // one competition per capture, all left in setup (or, for the draw
  // preview, draw-ready) status.
  setup: {
    seed: async ({ api }) => {
      await api.tournament({ name: TOURNAMENT_NAME, date: TOURNAMENT_DATE, durationDays: 1, courts: ['A', 'B'] });

      const menId = 'men-individual';
      await api.competition(menId, "Men's Individual", { date: TOURNAMENT_DATE, courts: ['A', 'B'] });

      const knockoutId = 'knockout-cup';
      await api.competition(knockoutId, 'Knockout Cup', { date: TOURNAMENT_DATE, format: 'knockout', courts: ['A', 'B'] });
      await api.participants(knockoutId, ROSTER_10);

      const overviewId = 'up-to-2nd-dan';
      await api.competition(overviewId, "Men's Individual: up to 2nd dan", { date: TOURNAMENT_DATE, courts: ['A', 'B'] });
      await api.participants(overviewId, ROSTER_18);

      const drawId = 'third-dan-and-above';
      await api.competition(drawId, "Men's Individual: 3rd dan and above", { date: TOURNAMENT_DATE, format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A', 'B'] });
      await api.participants(drawId, ROSTER_18);
      await api.generateDraw(drawId);

      return { menId, knockoutId, overviewId, drawId };
    },
  },

  // A separate tournament: a 4-team kachinuki knockout, so the schedule
  // estimator's best/average/worst RANGE (variable bout count under
  // kachinuki, admin_schedule_utils.jsx:11-25) has something to compute
  // over. The range itself is a pure function of format/teamSize/roster, not
  // of match progress, so the competition need not be started for the range
  // to render; it is started anyway (and one match closed via a fusensho
  // decision) so the "matches done"/"now"/progress tiles the committed shot
  // shows are real rather than the untouched draw-ready defaults.
  kachinuki: {
    seed: async ({ api }) => {
      await api.tournament({ name: 'Kachinuki Demo', date: TOURNAMENT_DATE, durationDays: 1, courts: ['A'] });

      const id = 'kachinuki-teams';
      await api.competition(id, 'Kachinuki Teams', {
        date: TOURNAMENT_DATE, format: 'knockout', courts: ['A'],
        teamSize: 5, teamMatchType: 'kachinuki',
      });
      await api.participants(id, [
        { name: 'Team Falcon', dojo: 'London' },
        { name: 'Team Dragon', dojo: 'Osaka' },
        { name: 'Team Tiger', dojo: 'Kyoto' },
        { name: 'Team Phoenix', dojo: 'Tokyo' },
      ]);
      await api.generateDraw(id);
      await api.start(id);

      // Close one of the two first-round matches so the Overview stats strip
      // reads "1/2 matches done" as the committed shot does. A fusensho
      // decision is format-agnostic (it ends the match outright, unlike
      // quick-score, which the server explicitly refuses for kachinuki -
      // "score bouts individually", handlers_match.go).
      try {
        const detail = await api.viewer(id);
        const matchId = detail?.bracket?.rounds?.[0]?.[0]?.id;
        if (!matchId) {
          throw new Error('the draw has no first-round match to close');
        }
        await api.post(`/api/competitions/${id}/matches/${encodeURIComponent(matchId)}/decision`, {
          decision: 'fusensho',
          decisionBy: 'shiro',
        });
      } catch (err) {
        // Do not swallow this. The capture's subject includes the "1/2 matches
        // done" and "50%" tiles, so a fixture that failed to close a match
        // would still photograph cleanly while showing the wrong figures.
        throw new Error(`kachinuki family: could not close a match, so the progress tiles would be wrong: ${err.message}`);
      }

      return { id };
    },
  },
};

export const recipes = [
  {
    name: 'mobile-create-competition',
    server: 'mobile',
    family: 'setup',
    viewport: { width: 1600, height: 1000 },
    dpr: 1,
    capture: 'viewport',
    waitFor: 'text=Add competition',
    setup: async ({ page, base }) => {
      await loginAdmin(page, base);
      await page.goto(base + '/admin/create-competition', { waitUntil: 'domcontentloaded' });
    },
    drive: async ({ page }) => {
      await fillFieldByLabel(page, 'Display name', "Men's Individual");
      // Default format is Knockout only (COMPETITION_DEFAULTS.format,
      // competition_shape.jsx:1202); the committed shot has "Pools +
      // Knockout" selected.
      await clickPill(page, 'Pools + Knockout');
    },
  },

  {
    name: 'mobile-add-participants',
    server: 'mobile',
    family: 'setup',
    viewport: { width: 1600, height: 1000 },
    dpr: 1,
    capture: 'viewport',
    waitFor: 'text=Participant list',
    setup: async ({ page, base, fixture }) => {
      await loginAdmin(page, base);
      await page.goto(base + `/admin/competition/${fixture.menId}/participants`, { waitUntil: 'domcontentloaded' });
    },
    // Types a roster into the paste box WITHOUT clicking "Apply changes":
    // the committed shot shows the "Unsaved changes" state (rosterDirty,
    // admin_participants.jsx:860) with the saved roster still at 0 players.
    drive: async ({ page }) => {
      const lines = [
        'Haruki Tanaka, Kenshinkan', 'Ren Suzuki, Musashi Dojo', 'Sota Yamamoto, Kenshinkan',
        'Yuto Watanabe, Seibukan', 'Riku Nakamura, Musashi Dojo', 'Kaito Kobayashi, Seibukan',
        'Daiki Yoshida, Hokushinkan', 'Hinata Kato, Kenshinkan', 'Sora Sasaki, Musashi Dojo',
        'Takumi Ito, Hokushinkan', 'Yuma Saito, Seibukan', 'Aoi Takahashi, Hokushinkan',
      ].join('\n');
      await page.locator('.lined-textarea__area').fill(lines);
    },
  },

  {
    name: 'mobile-participant-setup',
    server: 'mobile',
    family: 'setup',
    viewport: { width: 1265, height: 900 },
    dpr: 1,
    capture: 'fullPage',
    waitFor: 'text=Ordering & seeding',
    setup: async ({ page, base, fixture }) => {
      await loginAdmin(page, base);
      await page.goto(base + `/admin/competition/${fixture.knockoutId}/participants`, { waitUntil: 'domcontentloaded' });
    },
    // Seeds the first three of the ten already-added participants, matching
    // the committed shot's three highlighted seed rows.
    drive: async ({ page }) => {
      await setSeedRank(page, 'Aiko Sato', 1);
      await setSeedRank(page, 'Kenji Mori', 2);
      await setSeedRank(page, 'Yuki Sato', 3);
    },
  },

  {
    name: 'mobile-participants',
    server: 'mobile',
    family: 'setup',
    viewport: { width: 1280, height: 1000 },
    dpr: 2,
    capture: 'viewport',
    waitFor: 'text=Next steps',
    setup: async ({ page, base, fixture }) => {
      await loginAdmin(page, base);
      await page.goto(base + `/admin/competition/${fixture.overviewId}/overview`, { waitUntil: 'domcontentloaded' });
    },
  },

  {
    name: 'mobile-draw-preview',
    server: 'mobile',
    family: 'setup',
    viewport: { width: 1257, height: 900 },
    dpr: 2,
    capture: 'fullPage',
    waitFor: 'text=Draw ready',
    setup: async ({ page, base, fixture }) => {
      await loginAdmin(page, base);
      await page.goto(base + `/admin/competition/${fixture.drawId}/pools`, { waitUntil: 'domcontentloaded' });
    },
  },

  {
    name: 'kachinuki-estimate-range',
    server: 'mobile',
    family: 'kachinuki',
    viewport: { width: 920, height: 900 },
    dpr: 1,
    capture: 'viewport',
    waitFor: 'text=Schedule estimate',
    setup: async ({ page, base, fixture }) => {
      await loginAdmin(page, base);
      await page.goto(base + `/admin/competition/${fixture.id}/overview`, { waitUntil: 'domcontentloaded' });
    },
  },
];
