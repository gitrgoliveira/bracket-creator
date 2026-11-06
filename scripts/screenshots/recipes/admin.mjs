// Admin console captures riding the seeded demo tournament, and backup/restore captures.
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { demoTournament } from '../lib/seed.mjs';
import { VIEWER_SOURCES } from '../lib/scope.mjs';
import { settle, withAdminPage } from '../lib/ui.mjs';
import { PASSWORD } from '../lib/api.mjs';
import { EDITOR, TAP_DWELL_MS, finishMatch, startMatch } from '../lib/editor.mjs';

// Competitions and participant rosters used for backup and restore captures.
// Names and dojos follow the style in adminsetup.mjs with non-blank dojos.
const MEN_ID = 'men-individual';
const WOMEN_ID = 'women-individual';
const JUNIORS_ID = 'juniors-individual';

const ROSTER_MEN = [
  ['Daniel Brooks', 'Eikoku Kendo Club'],
  ['William Hill', 'Thames Kendo Kai'],
  ['Felix Bauer', 'Berlin Kenyu'],
  ['Sota Yamamoto', 'Seishinkan'],
  ['Riku Nakamura', 'Musashi Dojo'],
  ['Erik Lund', 'Nordic Kendo'],
  ['Haruki Tanaka', 'Kenshinkan'],
  ['Pierre Dubois', 'Paris Kendo Club'],
].map(([name, dojo]) => ({ name, dojo }));

const ROSTER_WOMEN = [
  ['Aiko Sato', 'Tokyo Kenyu'],
  ['Elena Rossi', 'Milano Kendo'],
  ['Hana Ito', 'Kyoto Budokan'],
  ['Sophie Martin', 'Paris Kendo Club'],
  ['Mio Abe', 'Seishinkan'],
  ['Karin Lind', 'Nordic Kendo'],
  ['Yui Endo', 'Kenshinkan'],
  ['Chloe Dupont', 'Lyon Kendo'],
].map(([name, dojo]) => ({ name, dojo }));

const ROSTER_JUNIORS = [
  ['Kenji Mori', 'Osaka Kenyu'],
  ['Ren Kato', 'Tokyo Budokan'],
  ['Sora Kimura', 'Seishinkan'],
  ['Taro Ono', 'Kyoto Budokan'],
  ['Sho Fujii', 'Musashi Dojo'],
  ['Daiki Takahashi', 'Kenshinkan'],
  ['Kaito Hayashi', 'Nenriki'],
  ['Ryota Saito', 'Berlin Kenyu'],
].map(([name, dojo]) => ({ name, dojo }));

// A fixed stamp for the stand-in daily copy, so the copy list reads the same on
// every run (the before-restore copy's stamp is the real clock, which no recipe
// can pin).
const DAILY_COPY_STAMP = '20260514T070000Z';

// Local helpers for driving the backup and restore interface.

// Upload an archive to the file input and wait for the dry-run plan to render.
async function uploadArchive(page, archivePath) {
  // The input is hidden behind the "Choose a backup file" button.
  const fileInput = page.locator('input[type="file"]');
  await fileInput.waitFor({ state: 'attached', timeout: 10000 });
  await fileInput.setInputFiles(archivePath);
  await page.locator('text=Restore plan').first().waitFor({ state: 'visible', timeout: 15000 });
}

// A competition's choices are one group, named by the competition; each
// label inside says only what it does ("Replace it"). The group's name starts
// with the competition's, so "Mens Individual" never finds "Womens Individual".
function choice(page, compName, label) {
  return page.getByRole('group', { name: new RegExp(`^${compName} `) }).getByLabel(label, { exact: true });
}

// Select "Replace" for an existing competition.
async function chooseReplace(page, compName) {
  await choice(page, compName, 'Replace it').click();
}

// Select "Restore as a copy" and fill the new copy name. The copy option wraps
// both the radio and the text box in one label, so the radio is found by its
// name and value, and the text box by its own accessible name.
async function chooseCopy(page, compId, compName, copyName) {
  const radio = page.locator(`input[name="decision-${compId}"][value="copy"]`);
  await radio.click();
  await page.getByLabel(`Name for the copy of "${compName}"`).fill(copyName);
}

// Select "Keep" for a competition missing from the backup archive.
async function chooseKeep(page, compName) {
  await choice(page, compName, 'Keep it').click();
}

// Answer the choices for the two competitions present in the backup archive.
// Used by both recipes to show Replace for Mens Individual and Copy for Womens Individual.
async function answerExistingChoices(page) {
  await chooseReplace(page, 'Mens Individual');
  await chooseCopy(page, WOMEN_ID, 'Womens Individual', 'Womens Individual (Copy)');
}

// A full-page capture taken while the page is scrolled draws the sticky top bar
// part way down the image, so every drive ends at the top.
async function scrollToTop(page) {
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForTimeout(150);
}

// Dismiss the scoring editor overlay if still visible.
async function dismissEditor(page) {
  const modal = page.locator(EDITOR).first();
  if (!(await modal.count())) return;
  const cancel = modal.locator('button').filter({ hasText: /^Cancel$/ }).first();
  if (await cancel.count()) {
    await cancel.click().catch(() => {});
  } else {
    await page.keyboard.press('Escape');
  }
  await modal.waitFor({ state: 'hidden', timeout: 5000 }).catch(() => {});
}

// The competition calendar's fixture: a two-day tournament on shiaijo A and B
// with an opening ceremony and a lunch every day, one drawn competition (its
// times come from the draw), one in setup that carries an expected number (an
// estimate), and a kachinuki competition on the second day. Built over the
// HTTP API, which is allowed here because the calendar shows placed blocks,
// not a bout or a point that an HTTP write could silently drop.
const CAL_DATE_1 = '16-05-2026';
const CAL_DATE_2 = '17-05-2026';
const CAL_DRAWN = "Men's Individual";
const CAL_SETUP = "Women's Individual";
const CAL_PAUSES = [
  { name: 'Opening ceremony', start: '09:00', length: '30m', day: 0 },
  { name: 'Lunch', start: '12:30', length: '1h', day: 0 },
];
const CAL_ROSTER = [
  ['Haruki Tanaka', 'Kenshinkan'], ['Ren Suzuki', 'Musashi Dojo'], ['Sota Yamamoto', 'Seishinkan'],
  ['Yuto Watanabe', 'Kenshinkan'], ['Riku Nakamura', 'Musashi Dojo'], ['Kaito Kobayashi', 'Seibukan'],
  ['Daiki Yoshida', 'Hokushinkan'], ['Hinata Kato', 'Kenshinkan'], ['Sora Sasaki', 'Musashi Dojo'],
  ['Takumi Ito', 'Hokushinkan'], ['Yuma Saito', 'Seibukan'], ['Aoi Takahashi', 'Hokushinkan'],
].map(([name, dojo]) => ({ name, dojo }));

async function seedCalendar({ api }) {
  await api.tournament({
    name: 'Spring Kendo Taikai 2026', date: CAL_DATE_1, durationDays: 2, courts: ['A', 'B'], pauses: CAL_PAUSES,
  });
  await api.competition('cal-mens', CAL_DRAWN, {
    date: CAL_DATE_1, courts: ['A', 'B'], startTime: '09:30',
  });
  await api.participants('cal-mens', CAL_ROSTER);
  await api.generateDraw('cal-mens');
  await api.competition('cal-womens', CAL_SETUP, {
    date: CAL_DATE_1, courts: ['A', 'B'], startTime: '14:00', expectedParticipants: 16,
  });
  await api.competition('cal-kachinuki', 'Kachinuki Teams', {
    date: CAL_DATE_2, courts: ['A'], startTime: '09:30', format: 'knockout',
    teamSize: 5, teamMatchType: 'kachinuki', expectedParticipants: 8,
  });
}

// The overlap fixture: the calendar's tournament on shiaijo A, B and C, with
// two competitions that start together at 10:00 on shiaijo C. The later one
// (by name) is drawn over the right half of the earlier one at its real time,
// and both are marked. Kept in a family of its own so the three captures above
// keep their two-shiaijo tournament.
const CAL_PAIR_FIRST = 'Kids Individual';
const CAL_PAIR_SECOND = 'Ladies Individual';

async function seedCalendarOverlap({ api }) {
  await api.tournament({
    name: 'Spring Kendo Taikai 2026', date: CAL_DATE_1, durationDays: 2, courts: ['A', 'B', 'C'], pauses: CAL_PAUSES,
  });
  await api.competition('cal-mens', CAL_DRAWN, {
    date: CAL_DATE_1, courts: ['A', 'B'], startTime: '09:30',
  });
  await api.participants('cal-mens', CAL_ROSTER);
  await api.generateDraw('cal-mens');
  await api.competition('cal-womens', CAL_SETUP, {
    date: CAL_DATE_1, courts: ['A', 'B'], startTime: '14:00', expectedParticipants: 16,
  });
  await api.competition('cal-kids', CAL_PAIR_FIRST, {
    date: CAL_DATE_1, courts: ['C'], startTime: '10:00', expectedParticipants: 16,
  });
  await api.competition('cal-ladies', CAL_PAIR_SECOND, {
    date: CAL_DATE_1, courts: ['C'], startTime: '10:00', expectedParticipants: 8,
  });
}

// The Move dialog of the setup competition, opened the way the page documents
// it: focus the block, press M. Dragging is not used because a recipe cannot
// show a drag in progress as a still, and the dialog is the same state a drag
// ends in.
async function openMoveDialog(page, name) {
  const block = page.getByRole('button', { name: new RegExp(`^${name},`) });
  await block.waitFor({ state: 'visible', timeout: 15000 });
  await block.focus();
  await page.keyboard.press('M');
  await page.getByTestId('cal-move-dialog').waitFor({ state: 'visible' });
  await page.getByLabel('Start time').waitFor({ state: 'visible' });
}

async function setMoveStart(page, value) {
  await page.getByLabel('Start time').fill(value);
  // The preview behind the dialog is debounced; its summary line carries
  // aria-live and appears once the new length is worked out.
  await page.locator('.cal-move-dialog [aria-live="polite"]').first().waitFor({ state: 'visible', timeout: 15000 });
}

export const families = {
  // The tournament the committed captures were taken against: "London Cup
  // Demo", six competitions. Seeded by the same script `make
  // mobile-app-example` runs, so the docs and the demo cannot drift apart.
  demo: {
    server: 'mobile',
    // What a SINCE-scoped run treats as this family's inputs (lib/scope.mjs):
    // the demo seed script and its CSVs, and the public viewer pages two of its
    // captures show. Shared modules are claimed by no family on purpose - a
    // change there runs everything. That includes admin.jsx, which routes every
    // admin page, and admin_shell.jsx, the dashboard's host and every admin
    // page's frame, although this family's dashboard capture shows them.
    sources: [
      'scripts/setup_tournament.py', 'test-data/', ...VIEWER_SOURCES,
    ],
    seed: async ({ base }) => {
      await demoTournament(base);
      return {};
    },
  },

  // The competition calendar and a move. One family per state, because a
  // recipe that confirms a move changes the fixture for the next one.
  calendar: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the calendar page, its move dialog
    // and rules, and the pause editor whose bands it draws.
    sources: ['web-mobile/js/admin_calendar', 'web-mobile/js/calendar_', 'web-mobile/js/pause_list'],
    seed: seedCalendar,
  },
  calendar_moved: {
    server: 'mobile',
    sources: ['web-mobile/js/admin_calendar', 'web-mobile/js/calendar_', 'web-mobile/js/pause_list'],
    seed: seedCalendar,
  },
  calendar_overlap: {
    server: 'mobile',
    sources: ['web-mobile/js/admin_calendar', 'web-mobile/js/calendar_', 'web-mobile/js/pause_list'],
    seed: seedCalendarOverlap,
  },

  backup: {
    server: 'mobile',
    sources: ['web-mobile/js/admin_backup.jsx'],
    seed: async ({ api, base, browser, dataDir }) => {
      // 1. Scaffolding via API: tournament and two initial competitions.
      // Separate courts avoid cross-competition court locks when running matches.
      await api.tournament({
        name: 'Spring Kendo Taikai 2026',
        courts: ['A', 'B', 'C'],
      });

      await api.competition(MEN_ID, 'Mens Individual', { courts: ['A'] });
      await api.participants(MEN_ID, ROSTER_MEN);
      await api.generateDraw(MEN_ID);
      await api.start(MEN_ID);

      await api.competition(WOMEN_ID, 'Womens Individual', { courts: ['B'] });
      await api.participants(WOMEN_ID, ROSTER_WOMEN);
      await api.generateDraw(WOMEN_ID);
      await api.start(WOMEN_ID);

      // 2. Take the backup snapshot while only the two initial competitions exist.
      const res = await fetch(`${base}/api/tournament/backup`, {
        headers: { 'X-Tournament-Password': PASSWORD },
      });
      if (!res.ok) {
        throw new Error(`failed to download backup: ${res.status} ${await res.text()}`);
      }
      const buffer = Buffer.from(await res.arrayBuffer());

      // Write archive to a temporary directory outside dataDir.
      const tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), 'bracket-backup-'));
      const archive = path.join(tmpDir, 'backup.zip');
      await fs.writeFile(archive, buffer);

      // Stand-in for the daily copy the app takes by itself, which the short
      // capture run cannot wait for. Scaffolding only: the page lists it by
      // name, kind and time.
      const backupsDir = path.join(dataDir, 'backups');
      await fs.mkdir(backupsDir, { recursive: true });
      await fs.writeFile(path.join(backupsDir, `${DAILY_COPY_STAMP}-scheduled.zip`), buffer);

      // 3. Mutate live state after backup: add Juniors Individual and start matches.
      await api.competition(JUNIORS_ID, 'Juniors Individual', { courts: ['C'] });
      await api.participants(JUNIORS_ID, ROSTER_JUNIORS);
      await api.generateDraw(JUNIORS_ID);
      await api.start(JUNIORS_ID);

      // Through the real interface, complete one match and start a second in Mens Individual,
      // and start one match in Juniors Individual.
      await withAdminPage(browser, { width: 1180, height: 820 }, async (page) => {
        // Complete match 1 in Mens Individual so the restore plan counts lost results.
        await page.goto(`${base}/admin/competition/${MEN_ID}/scores`, { waitUntil: 'domcontentloaded' });
        await page.locator('.score-edit-row').first().waitFor({ state: 'visible', timeout: 15000 });
        const firstScore = page.locator('.score-edit-row button').filter({ hasText: /^Score$/ }).first();
        await firstScore.click();
        await page.locator(EDITOR).first().waitFor({ state: 'visible', timeout: 15000 });
        await startMatch(page);
        for (let i = 0; i < 2; i += 1) {
          if (i > 0) await page.waitForTimeout(TAP_DWELL_MS);
          await page.locator('.sb-side--shiro .ipt-btn').filter({ hasText: /^M$/ }).first().click();
        }
        await page.waitForTimeout(250);
        await finishMatch(page);

        // Start match 1 in Juniors Individual and leave it running. Only the
        // Juniors competition runs a match: a restore that replaces or removes
        // a competition with a running match is refused until it is finished,
        // and the capture keeps Juniors, so its match is the one that may run.
        await page.goto(`${base}/admin/competition/${JUNIORS_ID}/scores`, { waitUntil: 'domcontentloaded' });
        await page.locator('.score-edit-row').first().waitFor({ state: 'visible', timeout: 15000 });
        const juniorScore = page.locator('.score-edit-row button').filter({ hasText: /^Score$/ }).first();
        await juniorScore.click();
        await page.locator(EDITOR).first().waitFor({ state: 'visible', timeout: 15000 });
        await startMatch(page);
        await dismissEditor(page);
      });

      return { archive, tmpDir };
    },
    teardown: async (fixture) => {
      if (fixture?.tmpDir) {
        await fs.rm(fixture.tmpDir, { recursive: true, force: true }).catch(() => {});
      }
    },
  },
};

export const recipes = [
  {
    name: 'mobile-dashboard',
    family: 'demo',
    route: '/admin',
    viewport: { width: 1280, height: 900 },
    dpr: 2,
    capture: 'viewport',
    auth: 'admin',
    waitFor: 'text=Admin console',
  },

  {
    name: 'mobile-calendar',
    family: 'calendar',
    route: '/admin/calendar',
    viewport: { width: 1280, height: 900 },
    capture: 'viewport',
    auth: 'admin',
    waitFor: `text=${CAL_DRAWN}`,
  },

  {
    // Two competitions starting together on shiaijo C: the second over the
    // right half of the first, both at their real times and both marked.
    name: 'mobile-calendar-overlap',
    family: 'calendar_overlap',
    route: '/admin/calendar',
    viewport: { width: 1280, height: 900 },
    capture: 'viewport',
    auth: 'admin',
    waitFor: `text=${CAL_PAIR_SECOND}`,
  },

  {
    // The calendar on a phone with a touch screen: one day at a time, a tab
    // for each day, and no Move button on any block.
    name: 'mobile-calendar-phone',
    family: 'calendar',
    route: '/admin/calendar',
    viewport: { width: 375, height: 812 },
    dpr: 2,
    touch: true,
    capture: 'viewport',
    auth: 'admin',
    waitFor: `text=${CAL_DRAWN}`,
  },

  {
    // The Move dialog on a competition that is not drawn yet, moved to 11:00 so
    // it lands on the drawn competition and the dialog warns about the overlap
    // without refusing it.
    name: 'mobile-calendar-move',
    family: 'calendar',
    route: '/admin/calendar',
    viewport: { width: 1280, height: 900 },
    capture: 'viewport',
    auth: 'admin',
    waitFor: `text=${CAL_SETUP}`,
    drive: async ({ page }) => {
      await openMoveDialog(page, CAL_SETUP);
      await setMoveStart(page, '11:00');
    },
  },

  {
    // The calendar straight after a move is confirmed: the block in its new
    // place, and the "Moved <name> just now. Undo puts it back where it was."
    // line with its Undo button above it.
    name: 'mobile-calendar-moved',
    family: 'calendar_moved',
    route: '/admin/calendar',
    viewport: { width: 1280, height: 900 },
    capture: 'viewport',
    auth: 'admin',
    waitFor: `text=${CAL_SETUP}`,
    drive: async ({ page }) => {
      await openMoveDialog(page, CAL_SETUP);
      await setMoveStart(page, '11:00');
      await page.getByTestId('cal-move-dialog').getByRole('button', { name: 'Move', exact: true }).click();
      await page.getByTestId('cal-moved-line').waitFor({ state: 'visible', timeout: 15000 });
    },
  },

  {
    name: 'backup-restore-plan',
    family: 'backup',
    route: '/admin/backup',
    viewport: { width: 1280, height: 900 },
    dpr: 1,
    capture: 'fullPage',
    auth: 'admin',
    waitFor: 'text=Back up tournament',
    drive: async ({ page, fixture }) => {
      await uploadArchive(page, fixture.archive);
      await answerExistingChoices(page);
      // Leave Juniors Individual unanswered so the open choice prompt and disabled button show.
      await settle(page);
      await scrollToTop(page);
    },
    assert: async ({ page }) => {
      // Both kinds of choice are on the page: replace-or-copy and keep-or-remove.
      await choice(page, 'Juniors Individual', 'Remove it').waitFor({ state: 'visible', timeout: 5000 });
      await choice(page, 'Mens Individual', 'Replace it').waitFor({ state: 'visible', timeout: 5000 });
      const openBtn = page.getByRole('button', { name: 'Choose what to do with "Juniors Individual"' });
      await openBtn.waitFor({ state: 'visible', timeout: 5000 });
      if ((await openBtn.getAttribute('aria-disabled')) !== 'true') {
        throw new Error('expected Restore button to be aria-disabled while a choice is open');
      }
    },
  },
  {
    name: 'backup-restore-done',
    family: 'backup',
    route: '/admin/backup',
    viewport: { width: 1280, height: 900 },
    dpr: 1,
    capture: 'fullPage',
    auth: 'admin',
    waitFor: 'text=Back up tournament',
    drive: async ({ page, fixture }) => {
      await uploadArchive(page, fixture.archive);
      await answerExistingChoices(page);
      await chooseKeep(page, 'Juniors Individual');

      const restoreBtn = page.getByRole('button', { name: 'Restore', exact: true });
      await restoreBtn.click();

      // Enter password in confirmation prompt dialog.
      const dialog = page.locator('.modal-backdrop--dialog');
      await dialog.waitFor({ state: 'visible', timeout: 5000 });
      const passwordInput = dialog.locator('input[type="password"]');
      await passwordInput.fill(PASSWORD);
      // A layer a tap opens ignores the bounce of that tap (useOpenedTapGuard),
      // so a click made at once does nothing; wait the guard out.
      await page.waitForTimeout(TAP_DWELL_MS + 400);
      await dialog.getByRole('button', { name: 'Restore', exact: true }).click();

      // Wait for restore confirmation and safety copy banner.
      await page.locator('text=Restore completed').first().waitFor({ state: 'visible', timeout: 20000 });
      await page.locator('text=Safety copy created:').first().waitFor({ state: 'visible', timeout: 5000 });
      await page.getByRole('button', { name: 'Undo this restore' }).waitFor({ state: 'visible', timeout: 5000 });

      // Wait for kept copies list to show both Daily and Before restore rows.
      const keptList = page.locator('.card', { has: page.locator('text=Copies kept on this computer') });
      await keptList.locator('.badge', { hasText: /^Daily$/ }).first().waitFor({ state: 'visible', timeout: 10000 });
      await keptList.locator('.badge', { hasText: /^Before restore$/ }).first().waitFor({ state: 'visible', timeout: 10000 });

      await settle(page);
      await scrollToTop(page);
    },
    assert: async ({ page }) => {
      // 1. Restore completed card with safety copy name.
      const restoreCard = page.locator('.card', { has: page.locator('text=Restore completed') });
      await restoreCard.locator('text=Safety copy created:').first().waitFor({ state: 'visible', timeout: 5000 });

      // 2. Undo this restore button.
      await page.getByRole('button', { name: 'Undo this restore' }).waitFor({ state: 'visible', timeout: 5000 });

      // 3. Kept copies list showing both Daily and Before restore rows.
      const keptList = page.locator('.card', { has: page.locator('text=Copies kept on this computer') });
      await keptList.locator('.badge', { hasText: /^Daily$/ }).first().waitFor({ state: 'visible', timeout: 5000 });
      await keptList.locator('.badge', { hasText: /^Before restore$/ }).first().waitFor({ state: 'visible', timeout: 5000 });
    },
  },
];
