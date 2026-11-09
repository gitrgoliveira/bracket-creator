// Calendar journey: the competition calendar on a desk (1280 px, a mouse) and
// on a phone (375 px, touch), over a two-day tournament on three shiaijo with
// an opening ceremony and a lunch pause.
//
// Seeded through the interface on the operator's iPad: the first-run form,
// Edit details (days and pauses) and the wizard (day, shiaijo, start time and
// expected participants, which size each block's estimate). Two competitions
// start at 10:00 on shiaijo C on day 2, the same-start pair.
//
// What it checks, through the page's own labels wherever a label says it:
//   a. Move then Undo with the Move button (desk): the block's label and the
//      day it sits on follow the move, "Moved <name> just now" appears, and
//      Undo puts the label back and says "Undone."
//   b. The phone: the grid frame does not scroll sideways when the shiaijo
//      fit, and no block shows a Move button.
//   c. The same-start pair, desk and phone: both blocks start on the 10:00
//      row, the second ends where its label says, and it is drawn on top.
//   d. Hovering the block the pair's second covers (desk): raised, its Move
//      button keeps clear of every line of text it shows.
//   e. The phone draws a short competition at its real length: its top and
//      bottom on its start and end rows, its name shown, no Move button of
//      its own, and a tap opens it.
//   f. The phone moves a block by press and hold, then a drag; Undo puts it
//      back.
//   g. A 768 px touch tablet keeps its Move buttons, each clear of its
//      block's name.
// A console error or a failed request fails the test (the console policy,
// fixtures/test.mjs).
import { test, expect } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { toastText } from '../fixtures/roster.mjs';
import {
  block, blockOpen, blockTimes, clockY, dayBody, intersects, lineBoxes, moveBlock, moveButton,
  holdAndDrag, movedLine, openCalendar, ownsItsCentre, press, setSchedule, undoMove,
} from '../fixtures/calendar.mjs';

const DESK = { viewport: { width: 1280, height: 900 }, hasTouch: false, isMobile: false, deviceScaleFactor: 1 };
const PHONE = { viewport: { width: 375, height: 900 }, hasTouch: true, isMobile: true, deviceScaleFactor: 1 };
const TABLET = { viewport: { width: 768, height: 1024 }, hasTouch: true, isMobile: true, deviceScaleFactor: 1 };

const MOVED = "Women's Individual";
const FIRST = 'Kids Individual';
const SECOND = 'Ladies Individual';
const PAIR_START = 10 * 60;
// A pixel or two: the block's border and sub-pixel rounding.
const SLACK_PX = 2;

// Open a signed-in calendar in a context of its own (the desk or the phone).
async function calendarOn(browser, baseURL, device) {
  const ctx = await browser.newContext({ ...device, baseURL });
  const page = await ctx.newPage();
  await login(page);
  await openCalendar(page);
  return { ctx, page };
}

// c: both blocks of the pair start on the 10:00 row, the second ends on the
// row its label names, and the second is the one drawn on top.
async function checkSameStartPair(page, scope) {
  const first = block(scope, FIRST);
  const second = block(scope, SECOND);
  await second.scrollIntoViewIfNeeded();
  const t1 = await blockTimes(scope, FIRST);
  const t2 = await blockTimes(scope, SECOND);
  expect(t1.start, t1.label).toBe(PAIR_START);
  expect(t2.start, t2.label).toBe(PAIR_START);
  const row = await clockY(page, PAIR_START);
  const end = await clockY(page, t2.end);
  const b1 = await first.boundingBox();
  const b2 = await second.boundingBox();
  expect(Math.abs(b1.y - row), `${FIRST} top ${b1.y} vs the 10:00 row ${row}`).toBeLessThanOrEqual(SLACK_PX);
  expect(Math.abs(b2.y - row), `${SECOND} top ${b2.y} vs the 10:00 row ${row}`).toBeLessThanOrEqual(SLACK_PX);
  expect(Math.abs(b2.y + b2.height - end), `${SECOND} bottom ${b2.y + b2.height} vs its end row ${end} (${t2.label})`)
    .toBeLessThanOrEqual(SLACK_PX);
  expect(await ownsItsCentre(second), `${SECOND} is drawn over ${FIRST}`).toBe(true);
}

test.describe.configure({ mode: 'serial' });

test.describe('calendar', () => {
  let shot;
  test.beforeAll(() => {
    shot = journeyShots('calendar');
  });

  test('seed a two-day tournament with pauses and a same-start pair', async ({ page }) => {
    await createTournament(page, { name: 'Calendar Cup', courts: 3, date: '2026-05-16' });
    await setSchedule(page, {
      days: 2,
      pauses: [
        { name: 'Opening ceremony', day: 1, start: '09:00', length: '30m' },
        { name: 'Lunch', day: 0, start: '12:30', length: '1h' },
      ],
    });
    const comps = [
      { name: "Men's Individual", day: 1, courts: ['A', 'B'], startTime: '09:30', expected: 32 },
      { name: MOVED, day: 1, courts: ['C'], startTime: '10:30', expected: 16 },
      { name: 'Veterans Individual', day: 2, courts: ['A', 'B'], startTime: '09:30', expected: 16 },
      // The same-start pair. The second is short (well under 100 minutes),
      // too short on a phone for the Move row under its name.
      { name: FIRST, day: 2, courts: ['C'], startTime: '10:00', expected: 16 },
      { name: SECOND, day: 2, courts: ['C'], startTime: '10:00', expected: 8 },
    ];
    for (const c of comps) await createCompetition(page, c);
  });

  test('a. desk: Move then Undo with the Move button', async ({ browser, baseURL }) => {
    const { ctx, page } = await calendarOn(browser, baseURL, DESK);
    try {
      const before = await blockTimes(dayBody(page, 1), MOVED);
      await shot(page, 'desk-before-move');

      await moveBlock(page, MOVED, { dayText: 'Day 2', start: '15:00' });
      await expect(blockOpen(dayBody(page, 2), MOVED)).toHaveAttribute('aria-label', /^Women's Individual, 15:00 to about \d\d:\d\d\./);
      await expect(blockOpen(dayBody(page, 1), MOVED)).toHaveCount(0);
      await expect(movedLine(page)).toContainText(`Moved ${MOVED} just now`);
      await shot(page, 'desk-moved');

      await undoMove(page);
      expect(await toastText(page, /^Undone\./)).toBe(`Undone. ${MOVED} is back where it was.`);
      await expect(blockOpen(dayBody(page, 1), MOVED)).toHaveAttribute('aria-label', before.label);
      await expect(blockOpen(dayBody(page, 2), MOVED)).toHaveCount(0);
      await shot(page, 'desk-undone');
    } finally {
      await ctx.close();
    }
  });

  test('b. phone: no sideways scroll, and no Move button in any block', async ({ browser, baseURL }) => {
    const { ctx, page } = await calendarOn(browser, baseURL, PHONE);
    try {
      for (const day of [1, 2]) {
        await press(page, page.getByRole('tab', { name: new RegExp(`^Day ${day}`) }));
        await expect(page.getByRole('tab', { name: new RegExp(`^Day ${day}`) })).toHaveAttribute('aria-selected', 'true');
        const frame = page.locator('.cal-grid-frame');
        const { scrollWidth, clientWidth } = await frame.evaluate((el) => ({ scrollWidth: el.scrollWidth, clientWidth: el.clientWidth }));
        expect(scrollWidth, `day ${day}: the grid frame scrolls sideways`).toBeLessThanOrEqual(clientWidth);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `day ${day}: the page scrolls sideways`).toBe(true);

        const blocks = dayBody(page, day).locator('[data-testid="cal-block"]');
        expect(await blocks.count(), `day ${day} has blocks`).toBeGreaterThan(0);
        await expect(blocks.getByRole('button', { name: /^Move / })).toHaveCount(0);
        await shot(page, `phone-day-${day}`);
      }
    } finally {
      await ctx.close();
    }
  });

  test('c. the same-start pair on the desk and on the phone', async ({ browser, baseURL }) => {
    const desk = await calendarOn(browser, baseURL, DESK);
    try {
      await checkSameStartPair(desk.page, dayBody(desk.page, 2));
      await shot(desk.page, 'desk-same-start');
    } finally {
      await desk.ctx.close();
    }
    const phone = await calendarOn(browser, baseURL, PHONE);
    try {
      await press(phone.page, phone.page.getByRole('tab', { name: /^Day 2/ }));
      await expect(blockOpen(dayBody(phone.page, 2), SECOND)).toBeVisible();
      await checkSameStartPair(phone.page, dayBody(phone.page, 2));
      await shot(phone.page, 'phone-same-start');
    } finally {
      await phone.ctx.close();
    }
  });

  test('d. desk: a covered block, raised by a hover, keeps Move clear of its lines', async ({ browser, baseURL }) => {
    const { ctx, page } = await calendarOn(browser, baseURL, DESK);
    try {
      const scope = dayBody(page, 2);
      const covered = block(scope, FIRST);
      await covered.scrollIntoViewIfNeeded();
      // Its left edge: the right half is under the pair's second.
      await blockOpen(scope, FIRST).hover({ position: { x: 8, y: 8 } });
      const move = moveButton(scope, FIRST);
      await expect(move).toBeVisible();
      // Raised: the hovered block is the one under its own Move button.
      await expect.poll(() => move.evaluate((btn) => {
        const r = btn.getBoundingClientRect();
        const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
        return !!hit && btn.contains(hit);
      })).toBe(true);
      const clash = async () => {
        const m = await move.boundingBox();
        return (await lineBoxes(covered)).filter((l) => intersects(l, m)).map((l) => l.text);
      };
      await expect.poll(clash, { message: 'lines under the raised Move button' }).toEqual([]);
      expect((await lineBoxes(covered)).length, 'the raised block shows its lines').toBeGreaterThan(1);
      await shot(page, 'desk-raised');
    } finally {
      await ctx.close();
    }
  });

  test('e. phone: a short competition at its real length, name only, opened by a tap', async ({ browser, baseURL }) => {
    const { ctx, page } = await calendarOn(browser, baseURL, PHONE);
    try {
      await press(page, page.getByRole('tab', { name: /^Day 2/ }));
      const scope = dayBody(page, 2);
      const short = block(scope, SECOND);
      await short.scrollIntoViewIfNeeded();
      const t = await blockTimes(scope, SECOND);
      expect(t.end - t.start, `${t.label} is under 100 minutes`).toBeLessThan(100);
      const box = await short.boundingBox();
      const top = await clockY(page, t.start);
      const bottom = await clockY(page, t.end);
      expect(Math.abs(box.y - top), `top ${box.y} vs its start row ${top}`).toBeLessThanOrEqual(SLACK_PX);
      expect(Math.abs(box.y + box.height - bottom), `bottom ${box.y + box.height} vs its end row ${bottom}`).toBeLessThanOrEqual(SLACK_PX);
      await expect(short.locator('.cal-block__name')).toBeVisible();
      expect((await lineBoxes(short)).map((l) => l.text).every((t) => t === SECOND), 'only its name shows').toBe(true);
      await expect(moveButton(short, SECOND)).toHaveCount(0);
      await shot(page, 'phone-short-name-only');
      await blockOpen(scope, SECOND).tap();
      await expect(page).toHaveURL(/\/admin\/competition\/[^/]+/);
      await expect(page.getByRole('heading', { level: 1, name: SECOND })).toBeVisible();
    } finally {
      await ctx.close();
    }
  });

  test('f. phone: press and hold, then drag, moves a block', async ({ browser, baseURL }) => {
    const { ctx, page } = await calendarOn(browser, baseURL, PHONE);
    try {
      const scope = dayBody(page, 1);
      const before = await blockTimes(scope, MOVED);
      // An hour down: the clock's own rows give the distance.
      const dy = (await clockY(page, before.start + 60)) - (await clockY(page, before.start));
      await holdAndDrag(page, blockOpen(scope, MOVED), { dy });
      await expect(movedLine(page)).toContainText(`Moved ${MOVED} just now`);
      const hh = (m) => `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
      await expect(blockOpen(scope, MOVED)).toHaveAttribute('aria-label', new RegExp(`^${MOVED}, ${hh(before.start + 60)} to about `));
      await shot(page, 'phone-dragged');
      await undoMove(page);
      await expect(blockOpen(scope, MOVED)).toHaveAttribute('aria-label', before.label);
    } finally {
      await ctx.close();
    }
  });

  test('g. touch tablet: Move buttons stay, clear of the names', async ({ browser, baseURL }) => {
    const { ctx, page } = await calendarOn(browser, baseURL, TABLET);
    try {
      const moves = page.locator('[data-testid="cal-block"] .cal-block__move-btn');
      expect(await moves.count(), 'the tablet shows Move buttons').toBeGreaterThan(0);
      const frames = page.locator('[data-testid="cal-block"]').filter({ has: page.locator('.cal-block__move-btn') });
      for (let i = 0; i < await frames.count(); i += 1) {
        const f = frames.nth(i);
        await f.scrollIntoViewIfNeeded();
        const name = await f.locator('.cal-block__name').boundingBox();
        const move = await f.locator('.cal-block__move-btn').boundingBox();
        expect(intersects(name, move), `${(await f.locator('.cal-block__name').innerText()).trim()}: Move over the name`).toBe(false);
      }
      await shot(page, 'tablet');
    } finally {
      await ctx.close();
    }
  });
});
