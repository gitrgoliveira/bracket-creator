// The roster surfaces an organiser works before and just after the draw: the
// Registration desk (check-in, its search, Group by dojo), the seed rank boxes
// and the participant Edit dialog on the Participants & seeds page, and a read
// of the draw as it stands (pool membership, bracket slots) on the admin
// preview or on the public viewer. Every selector for those lives here.
import { expect } from '@playwright/test';
import { dwell, rendered } from './pace.mjs';
import { EXPECT_TIMEOUT_MS } from './devices.mjs';

// ---- the Registration desk (admin_registration_desk.jsx) -------------------

// Open the desk from the dashboard card (it is a view of /admin, not a URL)
// and pick one competition on its rail, so each row carries that competition's
// own check-in box, number tag and Edit pencil.
export async function openDesk(page, compName) {
  await page.goto('/admin');
  await page.getByRole('button', { name: /^Registration desk/ }).tap();
  await expect(page.getByRole('heading', { level: 1, name: 'Registration desk' })).toBeVisible();
  const rail = page.getByRole('navigation', { name: 'Competitions' });
  const item = rail.getByRole('button').filter({ hasText: compName });
  await item.tap();
  await expect(item).toHaveAttribute('aria-current', 'true');
}

export const deskSearch = (page) => page.getByRole('textbox', { name: 'Search competitors' });
export const deskRows = (page) => page.locator('.rd-roster .rd-row');
export const deskRow = (page, name) => deskRows(page).filter({ has: page.locator('.rd-row__name', { hasText: new RegExp(`^${name}$`) }) });
export const deskCheck = (page, name) => deskRow(page, name).getByRole('checkbox');
// "<n> of <total>" and the noun beside it, as one line.
export const deskHeadline = (page) => page.locator('.rd-headline');

// Tap one row's check-in box and wait for the box to read the new state.
export async function toggleCheckIn(page, name, on = true) {
  const box = deskCheck(page, name);
  await box.tap();
  await expect(box).toHaveAttribute('aria-checked', String(on));
}

// Type into the desk's search box and wait for the list to draw the filter.
// The filter is local React state (no debounce, no request), so the box
// holding the text and two frames later is the list drawn from it; a caller
// that knows the result still asserts it (a row count, a row visible).
export async function searchDesk(page, text) {
  const box = deskSearch(page);
  await box.fill(text);
  await expect(box).toHaveValue(text);
  await rendered(page);
}

export async function groupByDojo(page, on = true) {
  const toggle = page.getByRole('button', { name: 'Group by dojo' });
  if ((await toggle.getAttribute('aria-pressed')) !== String(on)) await toggle.tap();
  await expect(toggle).toHaveAttribute('aria-pressed', String(on));
}

// "Check in all N" on one dojo's group header (Group by dojo must be on).
export async function bulkCheckInDojo(page, dojo) {
  const group = page.locator('.rd-group').filter({ has: page.locator('.rd-group__name', { hasText: new RegExp(`^${dojo}$`) }) });
  await group.getByRole('button', { name: /^Check in all \d+$/ }).tap();
  await expect(group.getByRole('button', { name: /^Check in all/ })).toHaveCount(0);
}

// ---- seed rank boxes (admin_participants.jsx) ------------------------------

export async function openParticipants(page, id) {
  await page.goto(`/admin/competition/${id}/participants`);
  await expect(page.getByRole('button', { name: 'Clear seeds' })).toBeVisible();
}

const seedBox = (page, name) => page.getByLabel(`Seed rank for ${name}`);

// Type a rank into a competitor's seed box. Each rank is saved as it is typed
// (the docs say so), so the box is read back after the page is reloaded.
export async function setSeed(page, name, rank) {
  const box = seedBox(page, name);
  // The box saves 200 ms after the last keystroke (or at once when it loses
  // focus with a change still waiting; a change already sent is not sent
  // again), and a person has gone on to the next box long after that.
  // Playwright goes on at once, and a reload then drops the save still
  // pending: wait for the save that carries this rank to be answered.
  const saved = page.waitForResponse((r) => {
    const req = r.request();
    if (req.method() !== 'PUT' || !r.ok()) return false;
    let body;
    try { body = req.postDataJSON(); } catch { return false; }
    return Array.isArray(body && body.players) && body.players.some((p) => p.name === name && p.seed === Number(rank));
  }, { timeout: EXPECT_TIMEOUT_MS });
  await box.fill(String(rank));
  await expect(box).toHaveValue(String(rank));
  await saved;
}

export const seedRankOf = (page, name) => seedBox(page, name).inputValue();

// ---- the participant Edit dialog -------------------------------------------

export const editDialog = (page) => page.locator('[role="dialog"][aria-labelledby="edit-modal-title"]');

export async function openEdit(page, name) {
  await page.getByRole('button', { name: `Edit ${name}` }).tap();
  const dlg = editDialog(page);
  await expect(dlg).toBeVisible();
  await expect(dlg.locator('#edit-modal-title')).toHaveText(`Edit ${name}`);
  return dlg;
}

// Fill the dialog and tap Save once the dialog's own bounce window has passed.
// Returns the dialog so a caller can see whether it closed (saved) or stayed
// (refused, with a toast).
export async function saveEdit(page, { name, dojo }) {
  const dlg = editDialog(page);
  const shown = Date.now();
  if (name !== undefined) await dlg.getByLabel(/^Name/).fill(name);
  if (dojo !== undefined) await dlg.getByLabel(/^Dojo/).fill(dojo);
  await dwell(page, shown);
  await dlg.getByRole('button', { name: 'Save', exact: true }).tap();
  return dlg;
}

// The toast's text, once it shows a message matching `re`.
export async function toastText(page, re) {
  const msg = page.locator('.toast.is-visible .toast__msg').filter({ hasText: re });
  await expect(msg).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
  return (await msg.innerText()).trim();
}

// ---- the draw as it stands -------------------------------------------------

// Pool name -> the names in it, read from the pools preview (admin or public
// viewer: both draw `.pool` cards).
export async function poolMembers(page) {
  await expect(page.locator('.pool').first()).toBeVisible();
  return page.locator('.pool').evaluateAll((pools) => Object.fromEntries(pools.map((p) => [
    p.querySelector('.pool__name').textContent.trim(),
    [...p.querySelectorAll('.pool__player-name')].map((e) => (e.querySelector('.numbered-name__text') || e).textContent.trim()),
  ])));
}

// Bracket slot -> { name, num, dojo }, read from the bracket (admin preview or
// public viewer). A slot is "<match id>:a|b"; the match id (m-r1-3) is the
// place in the tree, which a replaced competitor must keep.
//
// `until(slots)`, when given, is the state the caller is about to assert: the
// bracket is re-read until it holds (up to the expect timeout) instead of once
// after the first match shows, which a late refetch could still change. On a
// timeout the last read is returned, so the caller's own assertion names what
// is missing.
export async function bracketSlots(page, until) {
  await expect(page.locator('.bc-match').first()).toBeVisible();
  if (!until) return readBracketSlots(page);
  let slots = await readBracketSlots(page);
  try {
    await expect.poll(async () => {
      slots = await readBracketSlots(page);
      return until(slots);
    }, { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
  } catch { /* the caller's assertion on `slots` reports the difference */ }
  return slots;
}

function readBracketSlots(page) {
  return page.locator('.bc-match').evaluateAll((matches) => {
    const out = {};
    matches.forEach((m) => {
      ['a', 'b'].forEach((side) => {
        const el = m.querySelector(`.bc-side--${side}`);
        const name = el && el.querySelector('.numbered-name__text');
        // A later round shows "Winner of M1" until that match is played.
        if (!name || /^(Winner|Loser) of /.test(name.textContent.trim())) return;
        const num = el.querySelector('.num-prefix');
        const dojo = el.querySelector('.bc-dojo');
        out[`${m.getAttribute('data-match-id')}:${side}`] = {
          name: name.textContent.trim(),
          num: num ? num.textContent.trim() : '',
          dojo: dojo ? dojo.textContent.trim() : '',
        };
      });
    });
    return out;
  });
}

export const bracketNames = async (page) => Object.values(await bracketSlots(page)).map((s) => s.name);

export async function openCompetitionTab(page, id, tab) {
  await page.goto(`/admin/competition/${id}/${tab}`);
}

// Generate the draw of a competition whose check-in leaves some competitors
// out: the app asks first, naming how many are excluded. Answer Confirm after
// the dialog's own bounce window and return the sentence it asked.
export async function generateDrawExcluding(page, id, excluded) {
  if (!(await page.getByRole('button', { name: 'Generate draw' }).isVisible())) await page.goto(`/admin/competition/${id}`);
  await page.getByRole('button', { name: 'Generate draw' }).tap();
  const dlg = page.locator('.modal[role="dialog"]');
  await expect(dlg).toContainText(`${excluded} participants are not checked in and will be excluded from the draw`);
  const shown = Date.now();
  const asked = (await dlg.innerText()).replace(/\s+/g, ' ').trim();
  await dwell(page, shown);
  await dlg.getByRole('button', { name: 'Confirm' }).tap();
  await expect(page.getByRole('button', { name: 'Discard draw' })).toBeVisible();
  return asked;
}
