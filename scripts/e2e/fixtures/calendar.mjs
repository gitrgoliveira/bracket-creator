// The tournament's days and pauses (Edit details), and the competition
// calendar (/admin/calendar, admin_calendar.jsx): its blocks, its clock rows,
// the Move dialog, the "Moved ... just now" line and its Undo. Every selector
// for those lives here.
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import { expect } from '@playwright/test';
import { isCoarse } from './pointer.mjs';
import { dwell } from './pace.mjs';

// A touch screen taps, a mouse clicks: the calendar is walked on both.
export const press = async (page, loc) => ((await isCoarse(page)) ? loc.tap() : loc.click());

// Set the number of days and the pauses on the dashboard's Edit details page,
// and save. `pauses` is a list of { name, day (0 = every day), start 'HH:MM',
// length ('30m', '1h') }; the days are set first, since a pause offers its
// day only once the tournament has more than one.
export async function setSchedule(page, { days, pauses = [] }) {
  await page.goto('/admin');
  await press(page, page.getByRole('button', { name: 'Edit details' }));
  const daysBox = page.locator('#edit-days');
  await daysBox.fill(String(days));
  await expect(daysBox).toHaveValue(String(days));
  for (const [i, p] of pauses.entries()) {
    await press(page, page.getByRole('button', { name: '+ Add a pause' }));
    await page.locator(`#pause-${i}-name`).fill(p.name);
    if (days > 1) await page.locator(`#pause-${i}-day`).selectOption(String(p.day ?? 0));
    await page.locator(`#pause-${i}-start`).fill(p.start);
    await page.locator(`#pause-${i}-length`).fill(p.length);
  }
  await press(page, page.getByRole('button', { name: 'Save changes' }));
  await expect(page.getByRole('button', { name: 'Edit details' })).toBeVisible();
}

export async function openCalendar(page) {
  await page.goto('/admin/calendar');
  await expect(page.locator('.cal-grid-frame')).toBeVisible();
}

const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// A block's open control, named by its accessible label, which starts
// "<name>, HH:MM to [about ]HH:MM." (`within` narrows it to one day's body).
export const blockOpen = (scope, name) => scope.getByRole('button', { name: new RegExp(`^${escape(name)}, `) });

// The block frame around that control (it also holds the Move button). The
// `has` locator is matched inside each frame, so it is built from the page,
// not from `scope` (a day's body would then be looked for inside the block).
const pageOf = (scope) => (typeof scope.page === 'function' ? scope.page() : scope);
export const block = (scope, name) => scope.locator('[data-testid="cal-block"]').filter({ has: blockOpen(pageOf(scope), name) });

// One day's body: the blocks placed on tournament day `day` (1-based).
export const dayBody = (page, day) => page.locator(`.cal-day-body[data-cal-day="${day}"]`);

export const moveButton = (scope, name) => scope.getByRole('button', { name: `Move ${name}`, exact: true });

// "<name>, 10:00 to about 11:45." -> { start: 600, end: 705 } (minutes).
export async function blockTimes(scope, name) {
  const label = await blockOpen(scope, name).getAttribute('aria-label');
  const m = /^.+?, (\d\d):(\d\d) to (?:about )?(\d\d):(\d\d)\./.exec(label || '');
  if (!m) throw new Error(`blockTimes: no time range in "${label}"`);
  return { start: Number(m[1]) * 60 + Number(m[2]), end: Number(m[3]) * 60 + Number(m[4]), label };
}

// Where the clock column puts minute `mins` of the day, in page pixels, from
// the clock's own hour rows (so the scale is read off the page, not assumed).
export async function clockY(page, mins) {
  const cells = page.locator('.cal-clock-cell');
  const texts = await cells.allInnerTexts();
  const hour = Math.floor(mins / 60);
  const at = texts.findIndex((t) => t.trim() === `${String(hour).padStart(2, '0')}:00`);
  if (at < 0 || at + 1 >= texts.length) throw new Error(`clockY: no ${hour}:00 row and the next`);
  const a = await cells.nth(at).boundingBox();
  const b = await cells.nth(at + 1).boundingBox();
  return a.y + ((b.y - a.y) * (mins - hour * 60)) / 60;
}

// Open a block's Move dialog, set its day (an option whose label holds
// `dayText`) and start, and move it. Returns once the "Moved ... just now"
// line shows.
export async function moveBlock(page, name, { dayText, start }) {
  // A mouse sees a block's Move button only while it points at the block.
  if (!(await isCoarse(page))) await blockOpen(page, name).hover({ position: { x: 8, y: 8 } });
  await press(page, moveButton(page, name));
  const dlg = page.getByTestId('cal-move-dialog');
  await expect(dlg.locator('#cal-move-day')).toBeVisible();
  const shown = Date.now();
  if (dayText) {
    const sel = dlg.locator('#cal-move-day');
    const label = (await sel.locator('option').allInnerTexts()).find((t) => t.includes(dayText));
    if (!label) throw new Error(`moveBlock: no day option holds "${dayText}"`);
    await sel.selectOption({ label });
  }
  if (start) await dlg.locator('#cal-move-start').fill(start);
  const go = dlg.getByRole('button', { name: 'Move', exact: true });
  await expect(go).toBeEnabled();
  // The dialog ignores a tap inside the bounce window of its opening.
  await dwell(page, shown);
  await press(page, go);
  await expect(dlg).toHaveCount(0);
  await expect(movedLine(page)).toContainText(`Moved ${name} just now`);
}

export const movedLine = (page) => page.getByTestId('cal-moved-line');

export async function undoMove(page) {
  await press(page, movedLine(page).getByRole('button', { name: 'Undo' }));
  await expect(movedLine(page)).toHaveCount(0);
}

// The line boxes of the text a block shows: one rect per rendered line of
// every text node, kept to the part inside the block (a line the block clips
// is not shown) and dropped when it has no area.
export function lineBoxes(frame) {
  return frame.evaluate((el) => {
    const outer = el.getBoundingClientRect();
    const out = [];
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      if (!n.textContent.trim()) continue;
      if (n.parentElement.closest('button')) continue; // the Move button's own text
      const range = document.createRange();
      range.selectNodeContents(n);
      for (const r of range.getClientRects()) {
        const left = Math.max(r.left, outer.left);
        const right = Math.min(r.right, outer.right);
        const top = Math.max(r.top, outer.top);
        const bottom = Math.min(r.bottom, outer.bottom);
        if (right - left > 0.5 && bottom - top > 0.5) {
          out.push({ x: left, y: top, width: right - left, height: bottom - top, text: n.textContent.trim() });
        }
      }
    }
    return out;
  });
}

// Do two boxes ({ x, y, width, height }) share any area?
export const intersects = (a, b) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;

// Is the point at the centre of `loc` inside `frame` (the element a tap there
// would reach belongs to it)?
export function ownsItsCentre(frame) {
  return frame.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return !!hit && el.contains(hit);
  });
}

// The calendar's long press, read from its one source as pace.mjs reads the
// tap guard's window: a guessed hold would drift from the app unnoticed.
const LONG_PRESS_MS = (() => {
  const file = fileURLToPath(new URL('../../../web-mobile/js/calendar_move.jsx', import.meta.url));
  const m = /export const LONG_PRESS_MS = (\d+);/.exec(fs.readFileSync(file, 'utf8'));
  if (!m) throw new Error(`calendar: no LONG_PRESS_MS found in ${file}`);
  return Number(m[1]);
})();

// Press and hold `loc` with one finger past the calendar's long press, drag it
// `dy` pixels down in small steps, and let go: the touch screen's drag. Sent
// as raw touch events over CDP, which Chromium turns into the pointer events
// the calendar listens for (Playwright's touchscreen only taps).
export async function holdAndDrag(page, loc, { dy, steps = 12 }) {
  await loc.scrollIntoViewIfNeeded();
  const box = await loc.boundingBox();
  const x = Math.round(box.x + Math.min(12, box.width / 2));
  const y = Math.round(box.y + Math.min(10, box.height / 2));
  const cdp = await page.context().newCDPSession(page);
  try {
    const touch = (type, py) => cdp.send('Input.dispatchTouchEvent', {
      type, touchPoints: type === 'touchEnd' ? [] : [{ x, y: py, id: 1 }],
    });
    await touch('touchStart', y);
    // The press has to be held, not waited for: nothing on the page says the
    // long press has fired until the drag shows its ghost.
    await page.waitForTimeout(LONG_PRESS_MS + 250);
    for (let i = 1; i <= steps; i += 1) await touch('touchMove', Math.round(y + (dy * i) / steps));
    await touch('touchEnd');
  } finally {
    await cdp.detach();
  }
}
