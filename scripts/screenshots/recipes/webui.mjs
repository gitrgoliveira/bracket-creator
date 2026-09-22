// CLI web UI ("bracket-creator serve", web/index.html) captures.
//
// This server is stateless: there is no tournament API and no data dir to
// seed. State arrives by loading one of the page's own built-in sample
// datasets, via the "Small/Medium/Large Sample" buttons
// (web/js/app.js:584-594) — NOT by uploading a CSV. Evidence: the committed
// player-list/seeding/seeds-assigned screenshots show "Kevin Clark, Team
// Alpha", "Luke Rodriguez, Team Beta", ... which is the app's own baked-in
// medium sample text verbatim (web/js/app.js:465-482, 18 lines -> "18
// players"). None of the test-data/*.csv fixtures contain that text (grepped
// for "Kevin Clark"/"Team Alpha": zero hits — the repo's CSV fixtures use
// Game-of-Thrones/LOTR-style names instead), so the committed shots could not
// have come from an upload. Driving the sample button instead of authoring or
// picking a CSV is simpler and reproduces the committed pixels exactly.

export const families = {
  webui: {
    seed: async () => ({}),
  },
};

// The header version badge starts as "Loading..." and is filled in by an
// async fetch (fetchAppVersion, web/js/app.js:28-33, called against
// fetchAppStatus in web/js/api.js) with version.GetVersion() (cmd/serve.go),
// which is the git describe/branch string, NOT always a "vX.Y.Z" tag (a
// worktree build reports its branch name, e.g.
// "worktree-bridge-cse_...", confirmed against this build). So wait for the
// badge to stop reading the placeholder, not for a semver shape, so captures
// don't show "Loading...".
async function waitForVersionBadge(page) {
  await page.waitForFunction(() => {
    const el = document.getElementById('app-version');
    return !!el && el.textContent !== '' && el.textContent !== 'Loading...';
  });
}

// Switches to "Pools + Knockout" (reveals #poolOptionsSection, the
// 'change' listener at web/js/app.js:180-182) and loads the 18-participant
// medium sample (#loadMediumSample handler at web/js/app.js:588-590, calling
// setPlayerList -> validateParticipantListInput + calculateTimeEstimate,
// both synchronous). Waits for the resulting client-side validation panel
// (renderPlayerListValidation's success branch, web/js/app.js:308-313, class
// .validation-success on #playerListValidation) so the panel and the
// populated Time Estimator figures are on screen before capture.
async function loadPoolsMediumSample({ page }) {
  await waitForVersionBadge(page);
  await page.check('#pools');
  await page.click('#loadMediumSample');
  await page.locator('#playerListValidation.validation-success').waitFor({ state: 'visible' });
}

// The seeds modal is a Bootstrap `.fade` modal: showing/hiding it animates
// the backdrop + dialog opacity over ~300ms (default Bootstrap transition,
// no override in web/css/styles.css). `.seed-input` becomes Playwright-
// "visible" as soon as the fade STARTS, not once it finishes, so capturing
// right after that (or right after a hide() call, which flips the button
// label synchronously but still animates the dialog out) caught the
// transition mid-flight: a translucent modal double-exposed over the page
// behind it. Wait on Bootstrap's own 'shown.bs.modal' / 'hidden.bs.modal'
// events instead, which fire once the transition is actually done. Call this
// once per page (it just arms two flags) before opening the modal.
async function armSeedsModalTransitionFlags(page) {
  await page.evaluate(() => {
    const el = document.getElementById('seedsModal');
    window.__seedsModalShown = false;
    window.__seedsModalHidden = false;
    el.addEventListener('shown.bs.modal', () => { window.__seedsModalShown = true; });
    el.addEventListener('hidden.bs.modal', () => { window.__seedsModalHidden = true; });
  });
}

async function waitForSeedsModalShown(page) {
  await page.waitForFunction(() => window.__seedsModalShown === true);
}

async function waitForSeedsModalHidden(page) {
  await page.waitForFunction(() => window.__seedsModalHidden === true);
}

// Ranks the medium sample's first three rows (Kevin Clark, Luke Rodriguez,
// Michael Lewis) 1/2/3. Each row's rank field is `.seed-input`
// (web/js/app.js:865-869), one per participant in list order. Deliberately
// leaves focus in the last-filled input (nth(2), Michael Lewis) rather than
// blurring, matching the committed screenshot's focus ring there.
async function assignFirstThreeSeeds(page) {
  const inputs = page.locator('.seed-input');
  await inputs.first().waitFor({ state: 'visible' });
  await inputs.nth(0).fill('1');
  await inputs.nth(1).fill('2');
  await inputs.nth(2).fill('3');
}

export const recipes = [
  {
    // Default landing state: Knockout selected (the radio's default
    // `checked`, web/index.html:59), no participants loaded yet.
    name: 'webui-main',
    server: 'web',
    family: 'webui',
    route: '/',
    viewport: { width: 1265, height: 900 },
    dpr: 1,
    capture: 'fullPage',
    waitFor: '#loadMediumSample',
    drive: async ({ page }) => waitForVersionBadge(page),
  },
  {
    // Pools + Knockout, medium sample loaded: 18 players, pool options
    // visible, validation success panel, populated time estimate.
    name: 'webui-player-list',
    server: 'web',
    family: 'webui',
    route: '/',
    viewport: { width: 1265, height: 900 },
    dpr: 1,
    capture: 'fullPage',
    waitFor: '#loadMediumSample',
    drive: loadPoolsMediumSample,
  },
  {
    // Same base state, "Assign Participant Seeds" modal open with the first
    // three ranks entered. Viewport-only capture (the runner crops to
    // 1280x900, not the modal's full row list), matching the committed shot
    // being cut off partway down the table.
    name: 'webui-seeding-modal',
    server: 'web',
    family: 'webui',
    route: '/',
    viewport: { width: 1280, height: 900 },
    dpr: 1,
    capture: 'viewport',
    waitFor: '#loadMediumSample',
    drive: async (args) => {
      await loadPoolsMediumSample(args);
      await armSeedsModalTransitionFlags(args.page);
      await args.page.click('#manageSeeds');
      await waitForSeedsModalShown(args.page);
      await assignFirstThreeSeeds(args.page);
    },
  },
  {
    // Seeds saved and the modal closed: #manageSeeds relabels itself to
    // "3 Seeds Assigned" (web/js/app.js:918-925).
    name: 'webui-seeds-assigned',
    server: 'web',
    family: 'webui',
    route: '/',
    viewport: { width: 1265, height: 900 },
    dpr: 1,
    capture: 'fullPage',
    waitFor: '#loadMediumSample',
    drive: async (args) => {
      await loadPoolsMediumSample(args);
      await armSeedsModalTransitionFlags(args.page);
      await args.page.click('#manageSeeds');
      await waitForSeedsModalShown(args.page);
      await assignFirstThreeSeeds(args.page);
      await args.page.click('#saveSeedsBtn');
      await waitForSeedsModalHidden(args.page);
    },
  },
];
