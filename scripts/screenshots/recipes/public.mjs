// Public / spectator surfaces: everything a browser sees WITHOUT ever having
// authenticated as an operator. None of these recipes declares `auth`, and a
// fresh Playwright context has never logged in, so that is the whole of it.
//
// Two of the six captures (viewer-home, viewer-competition) reuse the `demo`
// family from admin.mjs - the same "London Cup Demo" tournament the rest of
// the docs describe. The other four need fixtures `demo` cannot provide
// (self-run mode is immutable at creation and incompatible with demo's
// officiated tournament; an encho/overtime mark; a live running match for
// the TV board) so they get their own dedicated families below.
//
// Fixture note: this file's families each create their own tournament, one of
// them in self-run mode. That used to require scheduling care because every
// mobile recipe shared one server; run.mjs now gives each FAMILY its own
// server and data dir, so the families cannot contaminate one another.
import { settle, PASSWORD, withAdminPage } from '../lib/ui.mjs';
import { client } from '../lib/api.mjs';
import { EDITOR, finishMatch, startMatch } from '../lib/editor.mjs';
import { assertIndividualBoutPoints, assertHanteiRecorded } from '../lib/fixture.mjs';
import { SCORE_EDITOR_SOURCES, VIEWER_SOURCES } from '../lib/scope.mjs';
import { families as adminFamilies } from './admin.mjs';

// The competition ids two seeds below create, at module level so the recipes
// that capture them can say so in `route` (run.mjs concatenates `base +
// route`, so the id has to be known when the recipe is declared).
const ENCHO_COMP = 'individual-cup';
const SELFRUN_COMP = 'open-individual';

// The FIRST row of /admin/score-editor. The editors group's own opener finds
// a row by the two competitors' names; the fixtures here only care about
// ORDER (first scheduled match, next scheduled match, ...), not identity.
const firstScoreRow = (page) => page.locator('.score-edit-row').first();

async function openScoreEditorRow(page, row) {
  await row.waitFor({ state: 'visible', timeout: 15000 });
  await row.locator('button', { hasText: /^(Score|Correct)$/ }).first().click();
  await page.locator(EDITOR).waitFor({ state: 'visible', timeout: 15000 });
}

async function tapIppon(page, side, waza = 'M') {
  const cls = side === 'shiro' ? '.sb-side--shiro' : '.sb-side--aka';
  await page.locator(cls).locator('button', { hasText: new RegExp(`^${waza}$`) }).first().click();
}

async function armEncho(page) {
  // EnchoControl (admin_scoring_shared.jsx): collapsed pill -> expand ->
  // check the "Encho started" box, which arms periodCount=1.
  //
  // Click the icon span, NOT [data-testid="scoring-modal-encho-pill"] itself
  // (confirmed by hand): the pill's own text is wrapped in a nested TermAS
  // glossary-term trigger, so a plain .click() on the pill lands on that
  // inner element's center and opens the "Overtime" glossary tooltip instead
  // of the setShowCounter(true) the outer button owns. The icon span is
  // outside the glossary term and reaches the outer button reliably.
  await page.locator('.encho-pill__icon').click();
  await page.locator('[data-testid="scoring-modal-encho-checkbox"]').click();
}

// Settle a TIED match by referee decision. The individual editor keeps this
// behind a two-step control (admin_scoring_individual.jsx:946-1003): "Decide by
// hantei..." arms it, then a SHIRO/AKA button commits - and that commit is also
// what advances to the next match, exactly as Finish does. The verdict is
// recorded as an "Ht" ippon in the winner's free slot, never as a centre mark.
async function decideByHantei(page, side) {
  await page.locator('[data-testid="scoring-modal-hantei-arm"]').click();
  await page.locator(`[data-testid="scoring-modal-hantei-${side}"]`).click();
}

export const families = {
  ...adminFamilies,

  // Self-run tournament with TWO individual competitions left in "setup"
  // status (no draw generated) so shouldShowRegister (viewer_home.jsx) stays
  // true:
  // mode === "self-run" && kind !== "team" && status is falsy/"setup".
  // A small roster is added so the public home page isn't bare, but nothing
  // needs scoring or a draw for either capture.
  selfRun: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the self-registration page and the
    // public viewer home.
    sources: ['web-mobile/js/registration', ...VIEWER_SOURCES],
    seed: async ({ api, base }) => {
      await api.tournament({
        name: 'Riverside Open',
        venue: 'Riverside Budokan',
        mode: 'self-run',
        adminPassword: PASSWORD,
      });
      const compId = await api.competition(SELFRUN_COMP, 'Open Individual', { courts: ['A'] });
      // A SECOND open competition, because selfrun-viewer-home's caption reads
      // "each open competition carries a Register button" - a plural claim that
      // one card demonstrates poorly, and that a reader cannot check at all.
      const secondId = await api.competition('womens-open', "Women's Open", { courts: ['A'] });
      // Setting a self-run tournament's adminPassword above flips
      // ElevatedVerifier.GateActive() on (middleware.go's RequireElevatedPassword):
      // a roster mutation is one of the gated "destructive ops" (delete
      // competition, discard draw, roster add/edit, import), so it needs a
      // SECOND header, X-Admin-Password, which the shared client sends when
      // asked. A plain roster POST is scaffolding, not a fidelity concern.
      const elevated = client(base, { 'X-Admin-Password': PASSWORD });
      await elevated.post(`/api/competitions/${compId}/participants`, { players: [
        { name: 'Emi Kondo', dojo: 'Riverside Dojo' },
        { name: 'Taro Fujita', dojo: 'Riverside Dojo' },
        { name: 'Nana Ishii', dojo: 'Harbor Dojo' },
        { name: 'Sho Matsuda', dojo: 'Harbor Dojo' },
      ] });
      await elevated.post(`/api/competitions/${secondId}/participants`, { players: [
        { name: 'Aiko Tanaka', dojo: 'Riverside Dojo' },
        { name: 'Mika Suzuki', dojo: 'Harbor Dojo' },
        { name: 'Hana Sato', dojo: 'Northgate Dojo' },
      ] });
      return { compId };
    },
  },

  // One league competition (single round-robin group, no pools/bracket split
  // to reason about), 4 participants -> 6 matches on one court, scheduled
  // sequentially. Scored via the real admin editor (fixture-fidelity rule):
  //   - match 1: tied 0-0, then encho armed and settled by one strike ->
  //     the "M (E) -" recent-result row this capture exists to show.
  //   - matches 2-3: a plain 2-0 win, to give "Recent results" more than one
  //     row (matches the shape of the surface, not a specific historical
  //     screenshot).
  //   - match 4: started but left unscored -> "ON NOW".
  //   - matches 5-6: left scheduled -> "Up next".
  enchoPool: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the public competition page it
    // captures, plus the score editor its seed drives to record the hantei.
    sources: [...VIEWER_SOURCES, ...SCORE_EDITOR_SOURCES],
    seed: async ({ api, base, browser }) => {
      await api.tournament({ name: 'Encho Cup', courts: ['A'] });
      const compId = await api.competition(ENCHO_COMP, 'Individual Cup', {
        courts: ['A'],
        format: 'league',
      });
      await api.participants(compId, [
        { name: 'Ren Yamada', dojo: 'Sakura Dojo' },
        { name: 'Kenji Ito', dojo: 'Sakura Dojo' },
        { name: 'Mika Suzuki', dojo: 'Kita Dojo' },
        { name: 'Aoi Watanabe', dojo: 'Kita Dojo' },
      ]);
      await api.start(compId);

      await withAdminPage(browser, null, async (page) => {
        await page.goto(base + '/admin/score-editor', { waitUntil: 'domcontentloaded' });

        // Open match 1 once. Its "Finish + Start Next ->" button (the
        // individual editor's chained-scoring CTA, admin_schedule_score_
        // editor.jsx's onSubmitAndNext) both completes the open match AND
        // starts the next scheduled one on the same shiaijo in a single
        // click, re-rendering the SAME modal onto that next match already
        // running - so matches 2-4 never need a fresh "Score" click from
        // the (covered-by-modal-backdrop) list underneath. Only match 1
        // needs an explicit "Start match" tap.
        await openScoreEditorRow(page, firstScoreRow(page));
        await startMatch(page);

        // Match 1: tied 0-0, then decided in encho by a single strike -
        // the "M (E) -" recent-result row this capture exists to show.
        await armEncho(page);
        await tapIppon(page, 'shiro', 'M');
        await settle(page);
        await finishMatch(page); // -> chains into match 2, already running

        // Match 2: a plain win, so "Recent results" has more than one row.
        await tapIppon(page, 'aka', 'M');
        await tapIppon(page, 'aka', 'K');
        await settle(page);
        await finishMatch(page); // -> chains into match 3, already running

        // Match 3: left level and settled by hantei. The alt text for this
        // capture promises "a hantei result with the Ht mark beside the
        // winner", and nothing here used to produce one - the shot shipped
        // without the very mark it exists to show.
        await decideByHantei(page, 'shiro');

        // Match 4 is now open and already running (chained by the match 3
        // finish above) -> "ON NOW". Leave it unscored and don't finish it.
        // Matches 5-6 are left untouched -> "Up next".
        await settle(page);
      });
      return { compId };
    },
  },

  // A POOLS competition, six participants over two pools of three, all on
  // shiaijo A. The caption for display-scoreboard promises "the current pool's
  // bouts with the running one highlighted ... and the next pool's bouts listed
  // below", which a knockout fixture cannot satisfy at all: this family used to
  // seed a 4-player knockout, so the board showed two semifinals and no pool
  // and no next-pool strip. Two pools are the minimum that gives the board a
  // current group AND a next one.
  liveCourt: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the TV display it captures, plus
    // the score editor its seed drives to start the bouts.
    sources: ['web-mobile/js/display', ...SCORE_EDITOR_SOURCES],
    seed: async ({ api, base, browser }) => {
      await api.tournament({ name: 'Court Board Demo', courts: ['A'] });
      const compId = await api.competition('board-demo', 'Board Demo Individual', {
        courts: ['A'],
        format: 'mixed',
        poolSize: 3,
        poolWinners: 2,
      });
      await api.participants(compId, [
        { name: 'Aiko Tanaka', dojo: 'Showa Dojo' },
        { name: 'Haruto Sato', dojo: 'Meiji Dojo' },
        { name: 'Emi Kondo', dojo: 'Sakura Dojo' },
        { name: 'Sho Matsuda', dojo: 'Kita Dojo' },
        { name: 'Yuki Mori', dojo: 'Showa Dojo' },
        { name: 'Ren Kobayashi', dojo: 'Meiji Dojo' },
      ]);
      await api.start(compId);

      await withAdminPage(browser, null, async (page) => {
        await page.goto(base + '/admin/score-editor', { waitUntil: 'domcontentloaded' });

        // Match 1: a plain 2-0 win, completed.
        await openScoreEditorRow(page, firstScoreRow(page));
        await startMatch(page);
        await tapIppon(page, 'aka', 'M');
        await tapIppon(page, 'aka', 'K');
        await settle(page);
        await finishMatch(page); // -> chains into match 2, already running

        // Match 2: started and given one ippon, then left running -> the
        // live scoreboard this capture exists to show.
        await settle(page);
        await tapIppon(page, 'shiro', 'M');
        await settle(page);
        // Left running on purpose - no finishMatch() call.
      });
      return { compId, court: 'A' };
    },
  },
};

export const recipes = [
  {
    name: 'viewer-home',
    family: 'demo',
    route: '/',
    viewport: { width: 804, height: 900 },
    capture: 'fullPage',
    // viewer_home.jsx:300, the tournament name header. Not
    // [data-testid="viewer-home-display-modes"] (viewer_home.jsx:535): that
    // testid sits inside a collapsed <details>, present in the DOM but not
    // "visible" per Playwright's default waitFor state, so it times out.
    waitFor: '.viewer__title--lg',
  },
  {
    name: 'viewer-competition',
    family: 'demo',
    route: '/competition/men-up-to-2d',
    viewport: { width: 815, height: 900 },
    capture: 'fullPage',
    // viewer_competition.jsx:326, the tab strip ("Overview"/"Pools"/...).
    waitFor: '.viewer__tab',
    // The caption promises "recent results with waza-level scores", and a
    // result written over the API carries no ippons, so those rows would
    // render blank while the page still looked entirely healthy.
    assert: ({ dataDir }) => { assertIndividualBoutPoints(dataDir, 'men-up-to-2d'); },
  },
  {
    name: 'viewer-result-encho',
    family: 'enchoPool',
    route: `/competition/${ENCHO_COMP}`,
    viewport: { width: 906, height: 900 },
    capture: 'fullPage',
    // viewer_competition.jsx: "Recent results" section-title, only rendered
    // once recentMatches is non-empty.
    waitFor: 'text=Recent results',
    // This capture exists to show a bout decided in encho, i.e. a scoreline,
    // AND a hantei result. Without ippons the (E) would sit beside two blank
    // cells; without an Ht the shot silently drops half its caption.
    assert: ({ dataDir }) => {
      assertIndividualBoutPoints(dataDir, ENCHO_COMP);
      assertHanteiRecorded(dataDir, ENCHO_COMP);
    },
  },
  {
    name: 'selfrun-register',
    family: 'selfRun',
    route: `/register/${SELFRUN_COMP}`,
    // Height is short on purpose: fullPage's own height isn't compared
    // (run.mjs only checks width for that mode) but a short viewport keeps
    // the page's min-height:100vh wrapper from padding the capture with a
    // few hundred px of empty grey below the card.
    viewport: { width: 380, height: 560 },
    dpr: 2,
    capture: 'fullPage',
    // registration.jsx:190, the "Register" h2.
    waitFor: 'text=Register',
  },
  {
    name: 'selfrun-viewer-home',
    family: 'selfRun',
    route: '/',
    // Short height for the same min-height:100vh reason as selfrun-register.
    viewport: { width: 380, height: 600 },
    dpr: 2,
    capture: 'fullPage',
    waitFor: '.viewer__title--lg',
  },
  {
    name: 'display-scoreboard',
    family: 'liveCourt',
    route: '/display?court=A',
    viewport: { width: 2560, height: 1440 },
    capture: 'viewport',
    // display_scoreboard.jsx:449, TvIndividualBoard's root.
    waitFor: '[data-testid="tv-display-root"]',
  },
];
