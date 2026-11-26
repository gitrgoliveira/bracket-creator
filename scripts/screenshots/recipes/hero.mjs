// The docs hero: ONE live bout on three screens at once.
//
//   hero         docs/screenshots/hero.png         1440 CSS px wide, DPR 2
//   hero-mobile  docs/screenshots/hero-mobile.png   600 CSS px wide, DPR 2
//
// The family seeds one tournament and leaves one bout running on shiaijo A,
// part scored. Both captures are of the STAGE (recipes/hero-stage.html), a
// page holding a live frame per surface, so the surfaces are three views of
// the same server at the same moment rather than three photographs stitched
// together afterwards:
//
//   1. the court console, /admin/shiaijo/A, at 1180x820 (a tablet), signed in;
//   2. the court display, /display?court=A, at 1280x720 (a TV);
//   3. a 390x844 phone that never signed in, following one competitor of the
//      running bout on its watchlist ("On court now").
//
// hero-mobile drops the console: under 600 px the landing page shows the
// display and the phone only. The "Watch it update live" clip records the same
// stage, so stills and clip share one composition (openStage below).
//
// Two origins, because the console must be signed in and the phone must not.
// The operator's sign-in is two localStorage keys (lib/ui.mjs), and a context's
// init script runs in EVERY frame, so authAdmin would sign the phone in too.
// The console is served from `localhost` and the display and phone from
// `127.0.0.1`, the same server under a second name, and the keys are set on
// the console's origin only (authAdmin's `origin`). A different origin has its
// own localStorage, so the phone starts as a spectator's browser does: empty.
import { londonCup } from '../lib/api.mjs';
import { authAdmin, courtConsoleRuns, pressInScaledFrame, settle, withAdminPage } from '../lib/ui.mjs';
import {
  INLINE_EDITOR, tapIppon, finishMatch, startNextFromList,
} from '../lib/editor.mjs';
import { assertIndividualBoutPoints } from '../lib/fixture.mjs';

// The stage is opened as the file it is, and then told what to frame
// (showStage), so it reads no URL from its own address. A file: page may frame
// both loopback names; a page the harness served by request interception may
// not, because it has no address of its own and Chromium's local network
// access checks then treat it as a public site (measured:
// net::ERR_BLOCKED_BY_LOCAL_NETWORK_ACCESS_CHECKS on the 127.0.0.1 frames).
// It is listed as the family's asset below, so a change to it re-runs the
// family on a scoped run.
const STAGE_URL = new URL('./hero-stage.html', import.meta.url);

const COMP = 'hero-individual';
const COURT = 'A';

// Two pools of three on shiaijo A: one pool's three bouts give the display a
// current group with results in it and the running bout highlighted, and the
// second pool its "Up next" strip.
const ROSTER = [
  ['Haruki Tanaka', 'Kenshinkan'],
  ['Erik Lund', 'Nordic Kendo'],
  ['Sota Yamamoto', 'Seishinkan'],
  ['Pierre Dubois', 'Paris Kendo Club'],
  ['Riku Nakamura', 'Musashi Dojo'],
  ['Oliver Grant', 'Eikoku Kendo Club'],
].map(([name, dojo]) => ({ name, dojo }));

// Two finished bouts, then the running one, scored through the real editor
// (an HTTP score carries no ippons). Sanbon shobu ends at two points, so the
// winner's second point is the LAST tap of a finished bout, and the running
// bout stops at one point each.
const FINISHED = [
  [['aka', 'M'], ['shiro', 'D'], ['aka', 'K']],
  [['shiro', 'M'], ['shiro', 'K']],
];
const RUNNING = [['aka', 'K'], ['shiro', 'M']];

async function scoreTaps(page, taps) {
  for (const [side, waza] of taps) await tapIppon(page, side, waza);
  await settle(page);
}

// Aka of the running bout, read back from the public competition payload, so
// the phone follows a competitor the seed really left on court. Resolved by
// the participant id the bout carries, then given the competitor number the
// phone types to find them (a number names one competitor; a name may not).
async function followedCompetitor(api) {
  const { poolMatches, pools } = await api.viewer(COMP);
  const live = (poolMatches || []).filter((m) => m.status === 'running');
  if (live.length !== 1) {
    throw new Error(`${COMP}: expected one running bout, found ${live.length}`);
  }
  const id = live[0].sideAId;
  const player = (pools || []).flatMap((p) => p.players || []).find((p) => id && p.id === id);
  if (!player || !player.number) {
    throw new Error(`${COMP}: the running bout's Aka (${id || 'no id'}) has no numbered roster entry`);
  }
  return { id, name: player.name, number: player.number };
}

export const families = {
  heroLive: {
    server: 'mobile',
    // No `sources`: the three surfaces are the court console, the display and
    // the public viewer, plus the editor the seed drives, which is most of the
    // application, so this group runs on every scoped run an application
    // change triggers.
    assets: ['hero-stage.html'],
    seed: async ({ api, base, browser, dataDir }) => {
      await londonCup(api);
      await api.competition(COMP, "Men's Individual", {
        courts: [COURT], numberPrefix: 'C', date: '19-09-2026', startTime: '09:00',
      });
      await api.participants(COMP, ROSTER);
      await api.start(COMP);

      await withAdminPage(browser, { width: 1280, height: 900 }, async (page) => {
        await page.goto(`${base}/admin/score-editor`, { waitUntil: 'domcontentloaded' });
        await startNextFromList(page);
        for (const taps of FINISHED) {
          await scoreTaps(page, taps);
          await finishMatch(page);
          await startNextFromList(page);
        }
        // Left running on purpose: no Finish.
        await scoreTaps(page, RUNNING);
      });
      assertIndividualBoutPoints(dataDir, COMP);
      return { compId: COMP, followed: await followedCompetitor(api) };
    },
  },
};

// Put the stage on `page` and wait until every frame shows the running bout.
// Exported for the "Watch it update live" clip, which records this same page.
export async function openStage(page, base, fixture, layout) {
  const consoleOrigin = new URL(base).origin;
  const publicBase = base.replace('//localhost:', '//127.0.0.1:');
  if (publicBase === base) throw new Error(`cannot derive a second origin from ${base}`);

  // The operator's two keys, on the console's origin only (see the header).
  await authAdmin(page.context(), { origin: consoleOrigin });

  const frames = {
    layout,
    display: `${publicBase}/display?court=${COURT}`,
    phone: `${publicBase}/`,
  };
  if (layout === 'wide') frames.console = `${base}/admin/shiaijo/${COURT}`;
  await page.goto(STAGE_URL.href, { waitUntil: 'domcontentloaded' });
  await page.evaluate((f) => window.showStage(f), frames);

  const { name, number } = fixture.followed;
  if (layout === 'wide') {
    const console = page.frameLocator('iframe[name="console"]');
    await console.locator(INLINE_EDITOR).first().waitFor({ state: 'visible', timeout: 20000 });
    await courtConsoleRuns(console);
    await console.getByText(name).first().waitFor({ state: 'visible', timeout: 15000 });
  }
  const display = page.frameLocator('iframe[name="display"]');
  await display.locator('[data-testid="tv-display-root"]').waitFor({ state: 'visible', timeout: 20000 });

  // The phone adds the competitor the way a spectator does: type their
  // competitor number into the watchlist box and pick them from the
  // suggestions, never by a pointer click (pressInScaledFrame, lib/ui.mjs).
  const phone = page.frameLocator('iframe[name="phone"]');
  const box = phone.getByPlaceholder(/name, dojo or number/i);
  await box.waitFor({ state: 'visible', timeout: 20000 });
  await box.fill(number);
  const options = phone.locator('button.pmf__option');
  await options.first().waitFor({ state: 'visible', timeout: 15000 });
  if ((await options.count()) !== 1) {
    throw new Error(`hero stage: "${number}" offers ${await options.count()} suggestions, expected ${name} alone`);
  }
  await pressInScaledFrame(options.filter({ hasText: name }));
  await phone.getByText(/on court now/i).first().waitFor({ state: 'visible', timeout: 20000 });
  await settle(page, 1200);
}

// The phone must be a spectator's and the console the operator's: read the
// sign-in keys from each frame's own storage, not from what the page draws.
// A signed-out app writes bc_authed "false" itself, so only "true" counts as
// signed in, and a spectator's frame must hold no password at all.
export async function assertStage(page, layout) {
  const keys = (frameName) => page.frame({ name: frameName })
    .evaluate(() => [localStorage.getItem('bc_authed'), localStorage.getItem('bc_password')]);
  for (const f of ['display', 'phone']) {
    const [authed, password] = await keys(f);
    if (authed === 'true' || password) throw new Error(`hero stage: the ${f} frame is signed in`);
  }
  if (layout === 'wide' && (await keys('console'))[0] !== 'true') {
    throw new Error('hero stage: the court console frame is not signed in');
  }
}

// One capture of the stage. Exported for the clip (recipes/videos.mjs), which
// spreads it and films the same composition.
export const stageRecipe = (name, layout, width) => ({
  name,
  family: 'heroLive',
  // Tall enough for either layout; the capture is the stage element itself,
  // so its size is the composition's, not the viewport's.
  viewport: { width, height: 1200 },
  dpr: 2,
  // A touch screen, as every surface on the stage is one (a tablet at the
  // court, a TV, a phone): the frames match (pointer: coarse) and lay out with
  // their touch-sized controls. The stage page itself sets a device-width
  // viewport, so the mobile emulation that comes with it leaves it unscaled.
  touch: true,
  capture: { selector: '#stage' },
  // No `auth`: the stage signs the console in on its own origin only.
  drive: ({ page, base, fixture }) => openStage(page, base, fixture, layout),
  assert: ({ page }) => assertStage(page, layout),
});

export const recipes = [
  stageRecipe('hero', 'wide', 1440),
  stageRecipe('hero-mobile', 'narrow', 600),
];
