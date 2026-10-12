// Two competitions on one shiaijo (bc-shcr) and a match moved between two
// courts (bc-mvsj): the seeding both journeys share, and the court console's
// cross-competition surfaces (the competition picker, the nudge lines, the
// "Another bout is running" alert, the done state) as one readable state.
//
// Seeding is UI-only, as every journey's is (README "Seeding goes through the
// interface"): the first-run form, the create wizard for EVERY competition
// before any roster is pasted (the realistic set-up order, and the one that
// decides each competition's default start time), then the roster paste box,
// Generate draw and Start for each. What the draw produced is read back from
// the Scores tab, never assumed.
import { expect } from '@playwright/test';
import { createTournament, login } from './setup.mjs';
import { createCompetition } from './wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from './competition.mjs';
import { hastyShiaijoConfirm } from './clumsy.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE } from './devices.mjs';
import { isCoarse, restoreCoarse } from './pointer.mjs';
import { INLINE_EDITOR } from './scoring.mjs';
import { UP_NEXT, WAITING_ROWS, completedRowFor, inlineEditor, waitingRows } from './shiaijo.mjs';
import * as T from './team.mjs';

// A device of its own: an operator iPad (pointer verified coarse, as the
// `page` fixture does for the test's own page) or a spectator phone, on this
// file's server. Each is a separate browser context, so two of them are two
// devices: separate storage, separate event streams. The caller closes
// `page.context()`.
export async function operatorDevice(browser, base) {
  const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL: base });
  const page = await ctx.newPage();
  if (!(await isCoarse(page))) await restoreCoarse(page);
  return page;
}
export async function publicDevice(browser, base) {
  const ctx = await browser.newContext({ ...PUBLIC_DEVICE, baseURL: base });
  return ctx.newPage();
}

// A mark left on a page's window, read back later: still there means the page
// was never reloaded or navigated away in between.
export const markPage = (page, mark) => page.evaluate((m) => { window.__e2eMark = m; }, mark);
export const pageMark = (page) => page.evaluate(() => window.__e2eMark || null);

// Sign in, creating the tournament first when this file's server has none.
export async function enterTournament(page, { name = 'Shared Court Cup', courts = 2 } = {}) {
  await page.goto('/');
  // The first-run form, or the public home of a tournament that exists:
  // whichever the first paint shows, however long it takes.
  const welcome = page.getByRole('heading', { name: 'Welcome to Bracket Creator' });
  const home = page.locator('.viewer__body');
  await welcome.or(home).first().waitFor({ state: 'visible' });
  if (await welcome.isVisible()) {
    await createTournament(page, { name, courts });
    return { created: true };
  }
  await login(page);
  return { created: false };
}

// What the create wizard offers before a competition is created: its default
// Start time (stacked after the latest same-day competition, admin_setup.jsx)
// and the hint under the shiaijo choice. Read on the wizard page itself, the
// one createCompetition then fills; the stacked default arrives from an
// estimate request, so the value is read once it has stopped changing.
export async function wizardDefaults(page) {
  await page.goto('/admin/create-competition');
  await expect(page.getByRole('heading', { level: 1, name: 'Add competition' })).toBeVisible();
  const input = page.locator('#create-start-time');
  let last = await input.inputValue();
  for (let i = 0; i < 6; i += 1) {
    await page.waitForTimeout(300);
    const now = await input.inputValue();
    if (now === last && i >= 2) break;
    last = now;
  }
  const hint = await page.locator('.field__hint', { hasText: 'Different competitions can share' }).first()
    .innerText().then((t) => t.trim(), () => null);
  return { startTime: last, sharedCourtHint: hint };
}

// Create every competition first, then paste each roster, generate its draw and
// start it. `specs` is [{ key, opts, roster }] (opts as createCompetition takes
// them). Returns { ids: {key: id}, defaults: {key: wizardDefaults()} }.
export async function seedCompetitions(page, specs) {
  const ids = {};
  const defaults = {};
  for (const s of specs) {
    defaults[s.key] = await wizardDefaults(page);
    ids[s.key] = await createCompetition(page, s.opts);
  }
  for (const s of specs) {
    await pasteRoster(page, ids[s.key], s.roster);
    await generateDraw(page, ids[s.key]);
    await startCompetition(page, ids[s.key]);
  }
  return { ids, defaults };
}

// Every row of a competition's Scores tab as the operator reads it: time,
// match label, court, both names and the button it offers. The pairings and
// courts the draw produced come from here.
export async function readScoresTab(page, id) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/scores`);
  await expect(page.locator('.score-edit-row').first()).toBeVisible();
  return page.locator('.score-edit-row').evaluateAll((rows) => rows.map((r) => {
    const txt = (sel) => {
      const el = r.querySelector(sel);
      return el ? el.textContent.replace(/\s+/g, ' ').trim() : '';
    };
    const names = [...r.querySelectorAll('.numbered-name__text')].map((n) => n.textContent.trim());
    const btn = [...r.querySelectorAll('button')].map((b) => b.textContent.trim()).find((t) => t === 'Score' || t === 'Correct') || '';
    return {
      time: txt('.score-edit-row__time'),
      label: txt('.score-edit-row__matchno'),
      court: txt('.score-edit-row__court').replace(/\s*▾$/, ''),
      shiro: names[0] || '',
      aka: names[1] || '',
      status: r.classList.contains('score-edit-row--running') ? 'running' : (r.classList.contains('score-edit-row--complete') ? 'completed' : 'scheduled'),
      button: btn,
    };
  }));
}

// The knockout slots a competition's Bracket tab prints on its cards ("M5
// SHIAIJO A 09:40"), including the matches still waiting on a feeder, which
// the Scores tab does not list.
export async function bracketSlots(page, id) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/bracket`);
  // The cards print the court in capitals through CSS, so the rendered text
  // (innerText) is what is read.
  const read = async () => (await page.locator('.page').first().innerText()).replace(/\s+/g, ' ');
  await expect.poll(read).toMatch(/M\d+ SHIAIJO \S+ \d\d:\d\d/);
  const text = await read();
  return [...text.matchAll(/M(\d+) SHIAIJO (\S+) (\d\d:\d\d)/g)].map((m) => ({ match: `M${m[1]}`, court: m[2], time: m[3] }));
}

// ---------------------------------------------------------------- court console

// The competition picker. It renders only while the court hosts more than one
// competition; with one, the officiating name is a plain heading.
export const competitionPicker = (page) => page.getByRole('combobox', { name: 'Select competition to officiate' });
export const officiatingName = async (page) => (await page.locator('.shiaijo-officiating__name').first().innerText()).replace('▾', '').trim();

// Pick a competition with the picker (a native select: chosen, not tapped, as
// the iPad's own picker wheel does it).
export async function pickCompetition(page, name) {
  await competitionPicker(page).selectOption({ label: name });
  await expect.poll(() => officiatingName(page)).toBe(name);
}

// The nudge lines (data-testid shiaijo-nudge): the quiet "also waiting" line
// while the shown competition still has work here, the amber "Switch to" once
// it has none. Each is a button that switches only when tapped.
export const nudges = (page) => page.getByTestId('shiaijo-nudge');
export async function nudgeLines(page) {
  return nudges(page).evaluateAll((els) => els.map((e) => ({
    text: (e.querySelector('.shiaijo-nudge__text') || e).textContent.trim(),
    quiet: e.classList.contains('shiaijo-nudge--also'),
    amber: e.classList.contains('alert--warn'),
  })));
}

// Everything the console says about what this court is doing, in one record
// for an audit row: the competition shown and the picker's choices, Up next,
// the queue, the live bout in the editor, the nudge lines, the "Another bout
// is running" alert and the done state.
export async function courtState(page) {
  return page.evaluate(({ UP_NEXT, WAITING_ROWS, INLINE_EDITOR }) => {
    const text = (el) => (el ? el.textContent.replace(/\s+/g, ' ').trim() : null);
    const names = (el) => (el ? [...el.querySelectorAll('.numbered-name__text')].map((n) => n.textContent.trim()) : []);
    const picker = document.querySelector('select[aria-label="Select competition to officiate"]');
    const card = document.querySelector(UP_NEXT);
    const ed = document.querySelector(INLINE_EDITOR);
    const rows = (sel) => [...document.querySelectorAll(sel)].map((r) => ({ head: text(r.querySelector('.shiaijo-qrow__time')), names: names(r) }));
    const alsoRunning = document.querySelector('.shiaijo-also-running');
    const empty = document.querySelector('.shiaijo__main > .empty:not(.shiaijo__placeholder)');
    return {
      officiating: text(document.querySelector('.shiaijo-officiating__name'))?.replace('▾', '').trim() || null,
      pickerOptions: picker ? [...picker.options].map((o) => o.textContent.trim()) : null,
      upNext: card ? { head: text(card.querySelector('.shiaijo-upnext__time')), names: names(card) } : null,
      upcoming: rows(WAITING_ROWS),
      later: rows('.shiaijo-pending .shiaijo-qrow'),
      completed: rows('.shiaijo-completed .shiaijo-qrow').length,
      editor: ed ? {
        eyebrow: text(ed.querySelector('.editor-modal__eyebrow')),
        court: ((ed.querySelector('.editor-modal__title') || {}).innerText || '').match(/Shiaijo\s+([A-Z]+)\b/i)?.[1] || null,
        title: ((ed.querySelector('.editor-modal__title') || {}).innerText || '').replace(/\s+/g, ' ').trim(),
        pill: text(ed.querySelector('.editor-head-pill')),
        names: names(ed).slice(0, 2),
      } : null,
      nudges: [...document.querySelectorAll('[data-testid="shiaijo-nudge"]')].map((e) => ({
        text: text(e.querySelector('.shiaijo-nudge__text')), quiet: e.classList.contains('shiaijo-nudge--also'),
      })),
      alsoRunning: alsoRunning ? text(alsoRunning) : null,
      done: empty ? text(empty) : null,
      idle: !!document.querySelector('.shiaijo__placeholder'),
    };
  }, { UP_NEXT, WAITING_ROWS, INLINE_EDITOR });
}

// The bout running in the console's editor, individual or team alike (the
// individual board's own side wrappers are not on the team sheet): its eyebrow,
// court and pairing, or null when the editor holds no running bout.
export async function liveBout(page) {
  const s = await courtState(page);
  if (!s.editor || s.editor.pill || s.editor.names.length < 2) return null;
  return { eyebrow: s.editor.eyebrow, court: s.editor.court, shiro: s.editor.names[0], aka: s.editor.names[1] };
}

// Correct a completed bout on the court console while a bout of EITHER
// competition is live there (individual.mjs correctCompleted reads the live
// bout off the individual board only). Correct is refused while a bout is live
// (bc-crpn): the notice at the row says so, the operator sends the live bout
// back to the queue (its score is kept) and taps Correct again. Returns the
// live bout and what the refusal said.
export async function correctAcross(page, pair) {
  const done = completedRowFor(page, pair);
  const live = await liveBout(page);
  await done.getByRole('button', { name: 'Correct' }).tap();
  let refusal = null;
  if (live) {
    const notice = done.locator('.shiaijo-upnext__error[role="alert"]');
    await expect(notice).toBeVisible();
    refusal = { notice: (await notice.innerText()).trim(), toast: (await page.locator('.toast').allInnerTexts().catch(() => [])).join(' | ') };
    expect(refusal.notice).toMatch(/^Shiaijo \S+ is running .+\. Finish it or send it back to the queue first\. Then correct this match\.$/);
    await page.getByRole('button', { name: 'Send back to queue' }).tap();
    refusal.sendBack = await hastyShiaijoConfirm(page);
    await expect.poll(() => liveBout(page), { timeout: 10_000 }).toBeNull();
    await done.getByRole('button', { name: 'Correct' }).tap();
  }
  await expect(inlineEditor(page).first().locator('.editor-head-pill')).toHaveText('CORRECTION');
  return { live, refusal };
}

// A waiting row of the queue (Upcoming, not the Up next card) naming both
// competitors of a pairing.
export const upcomingRow = (page, { shiro, aka }) => waitingRows(page)
  .filter({ hasText: shiro }).filter({ hasText: aka }).first();

// Open a row's court control (CourtPicker: "A ▾") and choose `court`. On the
// court console that opens the move confirm, left open for the caller; on the
// Scores tab and the schedule page the move is made at once.
export async function chooseCourt(row, court) {
  await row.locator('button[aria-haspopup="listbox"]').first().tap();
  const list = row.page().getByRole('listbox', { name: 'Choose shiaijo' });
  await expect(list).toBeVisible();
  await list.getByRole('option', { name: court, exact: true }).tap();
}

// The matches the tournament-wide Score editor lists as running ("Now") on
// `court`, from a page that may be anywhere (it is navigated there): the head
// table's count of what is being fought, independent of the court console.
export async function runningOnCourt(page, court) {
  await page.goto('/admin/score-editor');
  await expect(page.getByRole('heading', { level: 1, name: 'Score editor' })).toBeVisible();
  await page.getByRole('group', { name: 'Match status' }).getByRole('button', { name: 'Now' }).tap();
  await page.waitForTimeout(500);
  const rows = await page.locator('.score-edit-row').evaluateAll((els) => els.map((r) => {
    const names = [...r.querySelectorAll('.numbered-name__text')].map((n) => n.textContent.trim());
    const ct = r.querySelector('.score-edit-row__court');
    const comp = r.querySelector(':scope > div:first-child > div:last-child');
    return {
      court: ct ? ct.textContent.replace(/\s*▾$/, '').trim() : '',
      comp: comp ? comp.textContent.trim() : '',
      label: (r.querySelector('.score-edit-row__matchno') || {}).textContent || '',
      shiro: names[0] || '', aka: names[1] || '',
      status: r.classList.contains('score-edit-row--running') ? 'running' : 'other',
    };
  }));
  return rows.filter((r) => r.court === court && r.status === 'running');
}

// ---------------------------------------------------------------- team match

// Play the running TEAM match on the court console for its Shiro team (one men
// on every numbered bout) and Finish (+ Start Next) through the two-tap guard.
// Starts the Up next team match first when the court is idle. Returns the
// pairing played.
export async function playTeamMatch(page, { teamSize = 3 } = {}) {
  const ed = inlineEditor(page);
  if (!(await T.boutRow(ed, 1).isVisible().catch(() => false)) || (await ed.locator('.editor-head-pill').count()) > 0) {
    await T.startUpNextTeam(page);
  }
  const names = await ed.locator('.numbered-name__text').allInnerTexts();
  const pair = { shiro: names[0].trim(), aka: names[1].trim() };
  for (let n = 1; n <= teamSize; n += 1) {
    const row = T.boutRow(ed, n);
    if ((await T.rowSlots(row, 'shiro').count()) === 0 && (await T.rowSlots(row, 'aka').count()) === 0) {
      await T.boutIppon(ed, n, 'shiro', 'M');
    }
  }
  await T.finishTeam(ed);
  return pair;
}
