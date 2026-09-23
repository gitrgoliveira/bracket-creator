// The three docs VIDEOS, ported into the capture harness.
//
//   kachinuki-demo      docs/videos/kachinuki-demo.webm        820x1120  ~38s
//   draw-generation     docs/screenshots/draw-generation.webm  1160x620  ~7s
//   realtime-update     docs/screenshots/realtime-update.webm  440x900   ~6s
//
// A video recipe differs from a still one in two ways that shape everything
// below:
//
//   1. The camera is already rolling when setup() runs. The runner opens the
//      context with recordVideo and hands the recipe a live page, so anything
//      setup() does is ON FILM. Logging in through the form would put the login
//      screen in frame 0, so these recipes seed the two localStorage keys the
//      SPA reads instead (authAdmin, lib/ui.mjs). Everything else slow - creating the
//      tournament, scoring the matches that fill "Recent results" - belongs in
//      the family seed(), which gets `browser` and can open its own contexts
//      that no camera is pointed at.
//   2. Pacing is content. These are clips a reader watches, so the waits are
//      deliberate and are sized against the committed files' durations rather
//      than trimmed to the minimum the DOM needs.
import { authAdmin } from '../lib/ui.mjs';
import { EDITOR } from '../lib/editor.mjs';
import { SCORE_EDITOR_SOURCES, VIEWER_SOURCES } from '../lib/scope.mjs';

// ---------------------------------------------------------------------------
// Local helpers: the editor-driving ones stay here for the reason lib/ui.mjs
// gives in its header.
// ---------------------------------------------------------------------------

// The tournament every clip is recorded against. Each family gets a fresh
// server, so this is a plain create, and the courts are known here rather
// than read back afterwards.
const COURTS = ['A', 'B'];
const tournament = (api) => api.tournament({
  name: 'London Cup 2026', date: '19-09-2026', venue: 'London', durationDays: 1, courts: COURTS,
});

// An ippon button is a single letter (admin_scoring_individual.jsx:843,
// `ipt-btn`), so match the whole string: has-text("M") is a substring test and
// would be ambiguous the day a two-letter waza appears.
function ipponBtn(page, side, waza) {
  return page.locator(`.sb-side--${side} .ipt-btn`, { hasText: new RegExp(`^${waza}$`) }).first();
}

// The individual editor's commit button (admin_scoring_individual.jsx:1171 and
// :1179). It is a deliberate two-tap guard: the first tap arms it ("Tap again
// to finish"), the second submits. Its LABEL differs - the host passes
// onSubmitAndNext when another match on the same shiaijo is still active
// (admin_schedule_score_editor.jsx:409), which makes it "Finish + Start Next"
// - so target the button, not the words.
function finishBtn(page) {
  return page.locator(EDITOR).locator('.score-nav__actions .btn--primary').first();
}

async function finishMatch(page, gap = 550) {
  const btn = finishBtn(page);
  await btn.click();
  await page.waitForTimeout(gap);
  await btn.click();
  await page.waitForTimeout(gap * 2);
}

// ---------------------------------------------------------------------------
// kachinuki-demo
// ---------------------------------------------------------------------------
// Ported from a standalone scripts/record-kachinuki-demo.cjs, deleted once
// this recipe replaced it: it wrote straight into docs/videos/, bypassing the
// staging dir, and was a second recorder for the same clip. Its CSS injection
// and its keyboard scoring are kept; the server, the browser and the recording
// now come from the harness. Its seed wrote name-only lineups for teams called
// "Aka" and "Shiro", so the bout rows showed no competitor numbers; the teams
// below are real ones with numbered members.
const KACHI_KO = 'vid-kachinuki-ko';
const KACHI_LEAGUE = 'vid-kachinuki-league';

// Kachinuki is always the compact, internally-scrolled editor layout, so the
// whole encounter is only on screen once the height caps are lifted.
const EXPAND = '.modal-backdrop{align-items:flex-start!important;padding:8px 0!important}'
  + '.editor-modal--compact{max-height:none!important;height:auto!important}'
  + '.team-bouts-scroll{max-height:none!important;overflow:visible!important}';

// Each team, its dojo and its five fighters in fighting order. The fighters are
// named onto the numbered members the draw seeds and the lineup is written by
// member id (lib/api.mjs, nameMembers + lineup), so every bout row carries its
// competitor-number chip, as it does for a team entered on the Lineups page.
const KACHI_TEAMS = {
  'Team Kaze': { dojo: 'Kaze Dojo', fighters: ['Aoyama', 'Hirano', 'Iwata', 'Kondo', 'Murata'] },
  'Team Nami': { dojo: 'Nami Dojo', fighters: ['Hayashi', 'Ikeda', 'Kaneko', 'Noguchi', 'Okada'] },
  'Team Kita': { dojo: 'Kita Dojo', fighters: ['Sasaki', 'Takeda', 'Uchiyama', 'Yoshida', 'Hamada'] },
  'Team Minami': { dojo: 'Minami Dojo', fighters: ['Imai', 'Kojima', 'Miura', 'Nakano', 'Shimizu'] },
};

async function seedKachinukiComp(api, id, name, format, teamA, teamB, court) {
  // Through api.competition rather than a raw POST, so the competition gets
  // the start time every other seed gets.
  await api.competition(id, name, {
    format, teamSize: 5, teamMatchType: 'kachinuki', courts: [court],
  });
  await api.participants(id, [teamA, teamB].map((t) => ({ name: t, dojo: KACHI_TEAMS[t].dojo })));
  // The numbered members exist only once the draw has run.
  await api.generateDraw(id);
  for (const p of await api.get(`/api/competitions/${id}/participants`)) {
    await api.lineup(id, p.id, await api.nameMembers(id, p.id, KACHI_TEAMS[p.name].fighters));
  }
  await api.start(id);
  return id;
}

// ---------------------------------------------------------------------------
// draw-generation
// ---------------------------------------------------------------------------
const DRAW_COMP = 'vid-draw';

// 18 entrants over eight dojos: enough for six pools of three, and enough dojo
// spread that the draw has real work to do.
const DRAW_ROSTER = [
  ['Daniel Brooks', 'Eikoku Kendo Club'],
  ['William Hill', 'Thames Kendo Kai'],
  ['Felix Bauer', 'Berlin Kenyu'],
  ['Sota Yamamoto', 'Seishinkan'],
  ['Riku Nakamura', 'Musashi Dojo'],
  ['Erik Lund', 'Nordic Kendo'],
  ['Haruki Tanaka', 'Kenshinkan'],
  ['Pierre Dubois', 'Paris Kendo Club'],
  ['Hinata Kato', 'Seishinkan'],
  ['Ren Suzuki', 'Musashi Dojo'],
  ['Antoine Moreau', 'Paris Kendo Club'],
  ['Oliver Grant', 'Eikoku Kendo Club'],
  ['Lars Nilsen', 'Nordic Kendo'],
  ['Jonas Weber', 'Berlin Kenyu'],
  ['Kenji Mori', 'Kenshinkan'],
  ['Thomas Reed', 'Thames Kendo Kai'],
  ['Yuto Hayashi', 'Seishinkan'],
  ['Marc Leroy', 'Paris Kendo Club'],
].map(([name, dojo]) => ({ name, dojo }));

// ---------------------------------------------------------------------------
// realtime-update
// ---------------------------------------------------------------------------
const LIVE_COMP = 'vid-live';

const LIVE_ROSTER = [
  ['Erik Lund', 'Nordic Kendo'],
  ['Riku Nakamura', 'Musashi Dojo'],
  ['Sota Yamamoto', 'Seishinkan'],
  ['Pierre Dubois', 'Paris Kendo Club'],
  ['Haruki Tanaka', 'Kenshinkan'],
  ['Hinata Kato', 'Seishinkan'],
  ['Ren Suzuki', 'Musashi Dojo'],
  ['Antoine Moreau', 'Paris Kendo Club'],
  ['Daniel Brooks', 'Eikoku Kendo Club'],
  ['William Hill', 'Thames Kendo Kai'],
  ['Felix Bauer', 'Berlin Kenyu'],
  ['Lars Nilsen', 'Nordic Kendo'],
].map(([name, dojo]) => ({ name, dojo }));

// Score the open editor through the real board: waza letters land as ippons
// with their slots, which is what makes a result read "MK vs D" rather than a
// bare winner. Quick-scoring the same match over HTTP records no ippon arrays.
async function scoreOpenMatch(page, taps, gap = 220) {
  for (const [side, waza] of taps) {
    await ipponBtn(page, side, waza).click();
    await page.waitForTimeout(gap);
  }
}

// Open the competition's Scores tab and start its first unplayed match. The
// row's button reads "Score" until the match is complete and "Correct" after
// (admin_schedule_score_editor.jsx:290).
async function openFirstMatch(page, base, compId) {
  await page.goto(`${base}/admin/competition/${compId}/scores`, { waitUntil: 'domcontentloaded' });
  await page.locator('.score-edit-row').first().waitFor({ state: 'visible', timeout: 20000 });
  await page.locator('button.score-btn').first().click();
  await page.locator(EDITOR).waitFor({ state: 'visible', timeout: 15000 });
  const start = page.locator(EDITOR).locator('button', { hasText: /^Start match$/ }).first();
  if (await start.count()) {
    await start.click();
    await page.waitForTimeout(900);
  }
}

// The rows under "Recent results". That section renders LAST
// (viewer_competition.jsx:713), so it is the final .vsched group on the page.
const recentItems = (page) => page.locator('.vsched').last().locator('.vsched-item');

export const families = {
  // Two 2-team kachinuki competitions, started and ready to score: a knockout
  // (where a tie cannot end the encounter) and a league (where it can).
  videoKachinuki: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the competition scores page the
    // clip opens and the team editor it drives.
    sources: ['web-mobile/js/admin_competition', ...SCORE_EDITOR_SOURCES],
    seed: async ({ api }) => {
      await tournament(api);
      const court = COURTS[0];
      await seedKachinukiComp(api, KACHI_KO, 'KO Demo', 'knockout', 'Team Kaze', 'Team Nami', court);
      await seedKachinukiComp(api, KACHI_LEAGUE, 'League Demo', 'league', 'Team Kita', 'Team Minami', court);
      return { ko: KACHI_KO, lg: KACHI_LEAGUE };
    },
  },

  // A competition with its roster in and NO draw yet: generating it is the
  // subject of the clip, so the fixture has to stop one step short.
  videoDrawPending: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the competition overview page
    // where the draw is generated.
    sources: ['web-mobile/js/admin_competition'],
    seed: async ({ api }) => {
      await tournament(api);
      await api.competition(DRAW_COMP, "Men's Individual: 3rd dan and above", {
        courts: COURTS, numberPrefix: 'M', startTime: '09:00', date: '19-09-2026',
      });
      await api.participants(DRAW_COMP, DRAW_ROSTER);
      return { compId: DRAW_COMP };
    },
  },

  // A competition mid-session: two results already recorded, a third match
  // running, and an admin context parked on that match's editor. The parked
  // context is the SECOND screen the clip is about; it is opened here, off
  // camera, so the clip itself is only the push landing on the viewer.
  videoLive: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the public competition page the
    // clip watches update, plus the score editor its seed drives.
    sources: [...VIEWER_SOURCES, ...SCORE_EDITOR_SOURCES],
    // The seed leaves a signed-in page parked on the running match's editor
    // for the clip to drive; this closes it once the clip is recorded.
    teardown: ({ adminContext }) => adminContext.close(),
    seed: async ({ api, base, browser }) => {
      await tournament(api);
      // The last shiaijo, and only that one: the editor chains Prev/Next within
      // a court, and keeping this competition off the courts other fixtures use
      // keeps its running match from blocking their starts.
      const court = COURTS.slice(-1);
      // A number prefix is unique per tournament (the create returns 400 on a
      // reused one), and this file's draw fixture already holds "M".
      await api.competition(LIVE_COMP, "Men's Individual: 2nd dan and under", {
        courts: court, numberPrefix: 'N', startTime: '09:00', date: '19-09-2026',
      });
      await api.participants(LIVE_COMP, LIVE_ROSTER);
      await api.generateDraw(LIVE_COMP);
      await api.start(LIVE_COMP);

      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      await authAdmin(context);
      const adminPage = await context.newPage();
      await openFirstMatch(adminPage, base, LIVE_COMP);
      // Two finished results fill "Recent results"; each Finish lands on the
      // next match and starts it, so the third is left running with its editor
      // open - exactly the state the clip opens on.
      // Sanbon-shobu ends at two points, and the board disables both sides'
      // ippon buttons the moment a bout is decided, so the winner's second
      // point has to be the LAST tap in each list.
      for (const taps of [[['aka', 'M'], ['shiro', 'D'], ['aka', 'K']],
                          [['shiro', 'M'], ['aka', 'D'], ['shiro', 'K']]]) {
        await scoreOpenMatch(adminPage, taps, 120);
        await finishMatch(adminPage, 350);
      }
      await adminPage.locator(EDITOR).locator('.sb-side--aka').first()
        .waitFor({ state: 'visible', timeout: 15000 });
      return { compId: LIVE_COMP, adminPage, adminContext: context };
    },
  },
};

export const recipes = [
  {
    // Chapter times are printed
    // as CHAPTERS and are hand-synced into the numbered list under the <video>
    // in docs/user-guide/organisers/team-tournaments.md, so a re-record that
    // moves them means the prose moves too.
    name: 'kachinuki-demo',
    family: 'videoKachinuki',
    viewport: { width: 820, height: 1120 },
    capture: 'video',
    // No `route`: drive() navigates four times, and a navigation discards any
    // style sheet injected into the page it left. The runner has no css hook
    // for that reason; EXPAND is re-injected here after every navigation.
    auth: 'admin',
    drive: async ({ page, base, fixture }) => {
      // Frame 0 is roughly now: the page exists and has not navigated.
      fixture.t0 = Date.now();
      const { ko, lg } = fixture;
      const marks = [];
      const mark = (l) => marks.push([((Date.now() - fixture.t0) / 1000).toFixed(1), l]);
      // Not tolerant: without it the modal stays height-capped and the clip
      // frames a scrollbar instead of the board.
      const inject = () => page.addStyleTag({ content: EXPAND });
      const openScore = async (cid) => {
        await page.goto(`${base}/admin/competition/${cid}/scores`, { waitUntil: 'domcontentloaded' });
        await page.waitForTimeout(900);
        await page.locator('button.score-btn:has-text("Score"), button:has-text("Correct")').first().click();
        await page.waitForTimeout(750);
        await inject();
      };
      const startIfNeeded = async () => {
        const sm = page.locator('button:has-text("Start match")');
        if (await sm.count()) {
          // Found it, so a failure here is real: without the start the clip
          // records an encounter that never begins.
          await sm.first().click();
          await page.waitForTimeout(900);
        }
      };
      // The editor's global key handler is the sturdiest way to score without
      // hunting ippon buttons across a scrolled encounter: Shift+M is an Aka
      // men. It needs focus inside the board first.
      // Not tolerant: if focus fails the key presses go nowhere and the clip
      // records a board that never scores.
      const focus = () => page.locator('.sb-match, .team-summary').first().click({ timeout: 2000 });
      const aka = async (n = 1) => {
        await focus();
        for (let i = 0; i < n; i++) {
          await page.keyboard.press('Shift+M');
          await page.waitForTimeout(600);
        }
      };
      const click = async (sel, ms = 1100) => {
        await page.locator(sel).first().click({ timeout: 5000 });
        await page.waitForTimeout(ms);
      };
      // Tolerant variant, for controls that genuinely only exist in some
      // encounter states: the tie button is absent once the current bout is
      // scored. It must NOT be used for a control whose click IS a chapter's
      // subject - a swallowed Encho or Reopen yields a clip that plays
      // smoothly with a chapter missing, while the timestamps printed below
      // still get synced into the prose, which would then describe a moment
      // the clip does not contain.
      const clickT = async (sel, ms = 1100) => {
        await page.locator(sel).first().click({ timeout: 5000 }).catch(() => {});
        await page.waitForTimeout(ms);
      };
      const endMatch = async () => {
        // Ending is normally a two-tap guard: the first tap arms the button,
        // the second commits. A REOPENED encounter is different - there the
        // audit-reason prompt REPLACES the arm step and appears on the first
        // tap (admin_scoring_team.jsx:3825), so a blind second tap finds no
        // button. Branch on which of the two happened rather than swallowing
        // the failure: a clip whose encounter never ends is the wrong clip.
        const b = page.locator('[data-testid="kachinuki-end-match-button"]');
        const conf = page.locator('button:has-text("Confirm")');
        await b.click({ timeout: 5000 });
        await page.waitForTimeout(650);
        if (!(await conf.count())) {
          await b.click({ timeout: 5000 });
          await page.waitForTimeout(900);
        }
        if (await conf.count()) {
          await conf.first().click();
          await page.waitForTimeout(1300);
        } else {
          await page.waitForTimeout(600);
        }
      };

      // 1: winner stays on. Every fought bout reads "vs" in its centre.
      await openScore(ko);
      mark('P1 winner-stays');
      await aka(2); await click('button:has-text("Record bout")');
      await aka(2); await click('button:has-text("Record bout")');
      await page.waitForTimeout(1300);

      // 2: a knockout tie holds End match back and offers Encho, marked "(E)".
      mark('P2 knockout tie -> encho');
      await click('[data-testid="scoring-modal-tie-button"]', 1500);
      await click('button:has-text("Encho")', 1300);
      await aka(1);
      await page.waitForTimeout(800);
      await endMatch();

      // 3: the same tie in a league is simply ended as a draw, marked "X".
      mark('P3 league draw');
      await openScore(lg);
      await startIfNeeded();
      await clickT('[data-testid="scoring-modal-tie-button"]', 1400);
      await endMatch();

      // 4: a completed encounter reopens with its bouts intact, then ends
      // again through the reason prompt.
      mark('P4 reopen');
      await openScore(ko);
      await page.waitForTimeout(1200);
      await click('[data-testid="kachinuki-reopen-button"]', 1600); // closes the modal
      await openScore(ko);                                            // reopen the now-running match
      await page.waitForTimeout(900);
      await endMatch();
      mark('end');
      await page.waitForTimeout(700);

      console.log(`  kachinuki-demo CHAPTERS ${JSON.stringify(marks)}`);
      console.log('  (sync these into the numbered list under the <video> in '
        + 'docs/user-guide/organisers/team-tournaments.md)');
    },
  },

  {
    name: 'draw-generation',
    family: 'videoDrawPending',
    route: `/admin/competition/${DRAW_COMP}`,
    viewport: { width: 1160, height: 620 },
    capture: 'video',
    waitFor: 'button:has-text("Generate draw")',
    auth: 'admin',
    drive: async ({ page }) => {
      // Let the reader read the pre-draw header first. Shorter than it looks:
      // the clip opens on ~1.2s of the SPA's own "Loading…" screen, because a
      // recorded context films its page's very first navigation, and how long
      // that takes varies, so the pre-draw state gets a dwell of its own rather
      // than relying on the load to provide one.
      await page.waitForTimeout(1800);
      await page.locator('button', { hasText: /^Generate draw$/ }).first().click();
      // The status badge flips to "Draw ready" (ui.jsx:10) and the Pools
      // (preview) section takes over the body.
      await page.locator('.badge--draw-ready').first().waitFor({ state: 'visible', timeout: 30000 });
      await page.waitForTimeout(2800);
      // Scroll into the pools so the clip ends on real pairings rather than on
      // the toast.
      await page.evaluate(() => window.scrollBy({ top: 340, behavior: 'smooth' }));
      await page.waitForTimeout(1700);
    },
    assert: async ({ page }) => {
      const pools = await page.locator('.badge--draw-ready').count();
      if (!pools) throw new Error('draw-generation: the competition never reached draw-ready');
    },
  },

  {
    // The subject is an SSE push landing on a SECOND screen, so two contexts
    // are in play and only this one is recorded: the operator's context was
    // opened by the family seed and is driven from here through fixture.
    //
    // Kept LAST in this list on purpose. Its fixture parks a running match, and
    // the runner seeds a family immediately before its first recipe, so
    // anything this file starts happens after the other two are done.
    name: 'realtime-update',
    family: 'videoLive',
    route: `/competition/${LIVE_COMP}`,
    viewport: { width: 440, height: 900 },
    capture: 'video',
    // No `auth`: this context must never have logged in, or the SPA would
    // render the operator's view of the page instead of the public one.
    waitFor: '.section-title--running',
    drive: async ({ page, fixture }) => {
      const admin = fixture.adminPage;
      const before = await recentItems(page).count();

      // The viewer sits on the running match while the operator scores it.
      await page.waitForTimeout(1100);
      await scoreOpenMatch(admin, [['aka', 'M'], ['shiro', 'D'], ['aka', 'K']], 600);
      await finishMatch(admin, 380);

      // Wait on the push itself, not on a guess about how long it takes: the
      // row after the last one counted above appearing IS the push landing.
      await recentItems(page).nth(before).waitFor({ state: 'visible', timeout: 20000 });
      await page.waitForTimeout(1300);
    },
    assert: async ({ page }) => {
      const n = await recentItems(page).count();
      if (n < 3) throw new Error(`realtime-update: only ${n} recent results - the push never landed`);
    },
  },
];
