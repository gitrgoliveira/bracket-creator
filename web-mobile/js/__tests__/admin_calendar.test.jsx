import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { dirname, resolve } from 'path';
import { fileURLToPath } from 'url';
import {
  MOUSE_SLOP_PX,
  TOUCH_SLOP_PX,
  MIN_LANE_PX,
  CLOCK_COL_PX,
  PHONE_MAX_PX,
  PHONE_LANE_PX,
  PLACEHOLDER_MIN_PX,
  REFRESH_DEBOUNCE_MS,
  PX_PER_MINUTE,
  formatDayDate,
  getNextDayFormatted,
  unreadableCompetitionsText,
  shiftLanes,
  computeHourRange,
  computeDaysShown,
  computePaging,
  detectOffScreenOverlaps,
  computeBlocksLayout,
  CASCADE_PX,
  pauseLabelLane,
  fitBlockLines,
  touchMoveFits,
  MOVE_ROW_PX,
  NAME_ROW_PX,
  calendarLayout,
  withContentKeys,
} from '../admin_calendar.jsx';

describe('withContentKeys', () => {
  it('gives identical pauses distinct keys, and a unique one its own key', () => {
    const blank = { name: '', reason: 'no_start' };
    const lunch = { name: 'Lunch', reason: 'no_length' };
    const keyed = withContentKeys([blank, lunch, blank, blank], (u) => `${u.name}|${u.reason}`);
    expect(keyed.map((k) => k.key)).toEqual(['|no_start', 'Lunch|no_length', '|no_start#1', '|no_start#2']);
    expect(keyed.map((k) => k.item)).toEqual([blank, lunch, blank, blank]);
  });
});

describe('calendar day dates', () => {
  // The calendar's days carry Tournament.Days() dates, DD-MM-YYYY.
  it('formats a DD-MM-YYYY day and the day after it', () => {
    expect(formatDayDate('16-05-2026')).toBe('Sat 16 May');
    expect(getNextDayFormatted('16-05-2026')).toBe('Sun 17 May');
    expect(getNextDayFormatted('31-12-2026')).toBe('Fri 1 Jan');
  });

  it('gives nothing to show for a date that is not DD-MM-YYYY', () => {
    expect(formatDayDate('2026-05-16')).toBe('');
    expect(formatDayDate(undefined)).toBe('');
    expect(getNextDayFormatted('2026-05-16')).toBe('the next day');
  });
});

describe('unreadableCompetitionsText', () => {
  it('names one competition or several', () => {
    expect(unreadableCompetitionsText(['kids'])).toBe(
      'The settings of competition kids could not be read, so it is not on the calendar. Check its config.md file.');
    expect(unreadableCompetitionsText(['kids', 'teams'])).toBe(
      'The settings of 2 competitions could not be read (kids, teams), so they are not on the calendar. Check their config.md files.');
  });
});

describe('admin_calendar pure layout helper', () => {
  describe('exported constants', () => {
    it('matches required specification values', () => {
      expect(MOUSE_SLOP_PX).toBe(6);
      expect(TOUCH_SLOP_PX).toBe(10);
      expect(MIN_LANE_PX).toBe(110);
      expect(CLOCK_COL_PX).toBe(52);
      expect(PHONE_MAX_PX).toBe(720);
      expect(PHONE_LANE_PX).toBe(76);
      expect(PLACEHOLDER_MIN_PX).toBe(52);
      expect(REFRESH_DEBOUNCE_MS).toBe(1000);
      expect(PX_PER_MINUTE).toBe(1);
    });
  });

  describe('daysShown and lane width at 2, 4, 8 and 12 shiaijo on 1280 px', () => {
    const width = 1280; // gridWidth = 1280 - 52 = 1228
    const tournamentDays = 6;

    it('handles 2 shiaijo', () => {
      // 1228 / (2 * 110) = floor(1228 / 220) = 5 -> capped at min(4, 6) = 4
      const daysShown = computeDaysShown({ width, shiaijoCount: 2, tournamentDays });
      expect(daysShown).toBe(4);

      const layout = calendarLayout({
        calendar: {
          days: Array.from({ length: 6 }, (_, i) => ({ day: i + 1 })),
          courts: ['A', 'B'],
        },
        width,
      });
      expect(layout.daysShown).toBe(4);
      // 1228 / (4 * 2) = floor(1228 / 8) = 153 px
      expect(layout.laneWidth).toBe(153);
    });

    it('handles 4 shiaijo', () => {
      // 1228 / (4 * 110) = floor(1228 / 440) = 2
      const daysShown = computeDaysShown({ width, shiaijoCount: 4, tournamentDays });
      expect(daysShown).toBe(2);

      const layout = calendarLayout({
        calendar: {
          days: Array.from({ length: 6 }, (_, i) => ({ day: i + 1 })),
          courts: ['A', 'B', 'C', 'D'],
        },
        width,
      });
      expect(layout.daysShown).toBe(2);
      // 1228 / (2 * 4) = floor(1228 / 8) = 153 px
      expect(layout.laneWidth).toBe(153);
    });

    it('handles 8 shiaijo', () => {
      // 1228 / (8 * 110) = floor(1228 / 880) = 1
      const daysShown = computeDaysShown({ width, shiaijoCount: 8, tournamentDays });
      expect(daysShown).toBe(1);

      const layout = calendarLayout({
        calendar: {
          days: Array.from({ length: 6 }, (_, i) => ({ day: i + 1 })),
          courts: ['A', 'B', 'C', 'D', 'E', 'F', 'G', 'H'],
        },
        width,
      });
      expect(layout.daysShown).toBe(1);
      // 1228 / (1 * 8) = floor(1228 / 8) = 153 px
      expect(layout.laneWidth).toBe(153);
    });

    it('handles 12 shiaijo', () => {
      // 1228 / (12 * 110) = floor(1228 / 1320) = 0 -> clamped to 1
      const daysShown = computeDaysShown({ width, shiaijoCount: 12, tournamentDays });
      expect(daysShown).toBe(1);

      const courts = Array.from({ length: 12 }, (_, i) => String.fromCharCode(65 + i));
      const layout = calendarLayout({
        calendar: {
          days: Array.from({ length: 6 }, (_, i) => ({ day: i + 1 })),
          courts,
        },
        width,
      });
      expect(layout.daysShown).toBe(1);
      // lanes require at least MIN_LANE_PX (110 px), so dayGridWidth = 12 * 110 = 1320 px > 1228 px
      expect(layout.laneWidth).toBe(110);
      expect(layout.dayGridWidth).toBe(1320);
      expect(layout.lanesScrollSideways).toBe(true);
    });
  });

  describe('16 shiaijo on 1024 px (lanes scroll sideways)', () => {
    it('sets daysShown to 1, clamps laneWidth to MIN_LANE_PX, and marks sideways scroll', () => {
      const courts = Array.from({ length: 16 }, (_, i) => `Court ${i + 1}`);
      const layout = calendarLayout({
        calendar: {
          days: [{ day: 1 }, { day: 2 }],
          courts,
        },
        width: 1024,
      });
      expect(layout.daysShown).toBe(1);
      expect(layout.laneWidth).toBe(110);
      expect(layout.dayGridWidth).toBe(16 * 110); // 1760 px
      expect(layout.lanesScrollSideways).toBe(true);
    });
  });

  describe('phone width (one day, 76 px lanes)', () => {
    it('enforces 1 day and at least 76 px lanes on width <= 720 px or phone: true', () => {
      const courts = ['A', 'B', 'C', 'D'];
      const layout = calendarLayout({
        calendar: {
          days: [{ day: 1 }, { day: 2 }, { day: 3 }],
          courts,
        },
        width: 375,
        phone: true,
      });
      expect(layout.isPhone).toBe(true);
      expect(layout.daysShown).toBe(1);
      // four lanes fill the 375 px phone beside the 52 px clock column
      expect(layout.laneWidth).toBe(Math.floor((375 - 52) / 4));
    });

    it('keeps 76 px lanes and scrolls sideways when the shiaijo do not fit a phone', () => {
      const layout = calendarLayout({
        calendar: { days: [{ day: 1 }], courts: ['A', 'B', 'C', 'D', 'E', 'F'] },
        width: 375,
        phone: true,
      });
      expect(layout.daysShown).toBe(1);
      expect(layout.laneWidth).toBe(76);
      expect(layout.lanesScrollSideways).toBe(true);
    });
  });

  describe('hour range', () => {
    it('derives whole hours: earliest start less 1 h to latest end plus 1 h', () => {
      const calendar = {
        competitions: [
          {
            id: 'c1',
            placement: {
              placed: true,
              windows: {
                A: { start: 570, end: 990 }, // 09:30 to 16:30
              },
            },
          },
        ],
      };
      // 570 - 60 = 510 -> floor(510 / 60) = 8
      // 990 + 60 = 1050 -> ceil(1050 / 60) = 18
      const hr = computeHourRange(calendar);
      expect(hr.startHour).toBe(8);
      expect(hr.endHour).toBe(18);
      expect(hr.hours).toEqual([8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18]);
      expect(hr.totalMinutes).toBe(600);
    });
  });

  describe('paging', () => {
    it('pages days in groups and computes correct labels and boundaries', () => {
      const days = Array.from({ length: 6 }, (_, i) => ({ day: i + 1 }));
      const p1 = computePaging({ days, daysShown: 4, page: 0 });
      expect(p1.totalPages).toBe(2);
      expect(p1.currentPage).toBe(0);
      expect(p1.hasPrev).toBe(false);
      expect(p1.hasNext).toBe(true);
      expect(p1.label).toBe('Days 1 to 4 of 6');
      expect(p1.visibleDays.map(d => d.day)).toEqual([1, 2, 3, 4]);

      const p2 = computePaging({ days, daysShown: 4, page: 1 });
      expect(p2.currentPage).toBe(1);
      expect(p2.hasPrev).toBe(true);
      expect(p2.hasNext).toBe(false);
      expect(p2.label).toBe('Days 5 to 6 of 6');
      expect(p2.visibleDays.map(d => d.day)).toEqual([5, 6]);
    });
  });

  describe('off-screen overlap detection', () => {
    it('detects an overlap on a hidden day and formats the label', () => {
      const calendar = {
        days: Array.from({ length: 6 }, (_, i) => ({ day: i + 1 })),
        competitions: [
          {
            id: 'c1',
            name: 'Senior Individual',
            placement: {
              placed: true,
              day: 6,
              windows: { B: { start: 600, end: 720 } },
            },
            clashPartners: ['c2'],
          },
          {
            id: 'c2',
            name: 'Junior Individual',
            placement: {
              placed: true,
              day: 6,
              windows: { B: { start: 660, end: 780 } },
            },
            clashPartners: ['c1'],
          },
        ],
      };

      // When visible days are Days 1..4 (page 0), Day 6 is off screen
      const currentPageDays = [{ day: 1 }, { day: 2 }, { day: 3 }, { day: 4 }];
      const offScreen = detectOffScreenOverlaps({ calendar, currentPageDays });
      expect(offScreen).toHaveLength(1);
      expect(offScreen[0].day).toBe(6);
      expect(offScreen[0].label).toBe('Overlap on Day 6, Shiaijo B');

      // With the tournament's dates the label names the day the way the rest
      // of the page does.
      const dated = detectOffScreenOverlaps({
        calendar: { ...calendar, days: [{ day: 6, date: '21-05-2026' }] },
        currentPageDays,
      });
      expect(dated[0].label).toBe('Overlap on Thu 21 May (Day 6), Shiaijo B');
    });
  });

  describe('placeholder min height', () => {
    it('ensures placeholder blocks have at least PLACEHOLDER_MIN_PX height', () => {
      const calendar = {
        competitions: [
          {
            id: 'p1',
            name: 'New Division',
            placement: {
              placed: true,
              day: 1,
              courts: ['A'],
              lengthSource: 'minimum',
              lengthReason: 'no_participants',
              windows: { A: { start: 600, end: 620 } }, // 20 minutes
            },
          },
        ],
      };

      const blocks = computeBlocksLayout({
        calendar,
        visibleDays: [{ day: 1 }],
        courts: ['A'],
        laneWidth: 150,
        startHour: 8,
        endHour: 18,
      });

      expect(blocks).toHaveLength(1);
      expect(blocks[0].isPlaceholder).toBe(true);
      expect(blocks[0].height).toBe(PLACEHOLDER_MIN_PX); // 52 px despite 20 min span
    });
  });

  describe('clipping past midnight', () => {
    it('clips visual height at midnight (1440 mins) and flags pastMidnight', () => {
      const calendar = {
        competitions: [
          {
            id: 'm1',
            name: 'Night Championship',
            placement: {
              placed: true,
              day: 1,
              courts: ['A'],
              pastMidnight: true,
              windows: { A: { start: 1320, end: 1480 } }, // 22:00 to 00:40 (+1d)
            },
          },
        ],
      };

      const blocks = computeBlocksLayout({
        calendar,
        visibleDays: [{ day: 1 }],
        courts: ['A'],
        laneWidth: 150,
        startHour: 8,
        endHour: 24,
      });

      expect(blocks).toHaveLength(1);
      expect(blocks[0].pastMidnight).toBe(true);
      expect(blocks[0].effectiveEnd).toBe(1440);
      expect(blocks[0].height).toBe((1440 - 1320) * PX_PER_MINUTE); // 120 px
    });
  });

  describe('overlapping blocks cascade', () => {
    const comp = (id, name, courts, start, end) => ({
      id, name,
      placement: { placed: true, day: 1, courts, lengthSource: 'estimate', windows: Object.fromEntries(courts.map((c) => [c, { start, end }])) },
    });
    const layout = (competitions) => computeBlocksLayout({
      calendar: { competitions }, visibleDays: [{ day: 1 }], courts: ['A', 'B', 'C'], laneWidth: 200, startHour: 8, endHour: 18,
    });

    it('draws the later block after the earlier one, inset from the left, on a shared lane', () => {
      const blocks = layout([comp('late', 'Later', ['A'], 690, 760), comp('early', 'Earlier', ['A', 'B'], 600, 780)]);
      expect(blocks.map((b) => b.comp.id)).toEqual(['early', 'late']);
      expect(blocks[0].left).toBe(0);
      expect(blocks[0].width).toBe(400);
      expect(blocks[1].left).toBe(CASCADE_PX);
      expect(blocks[1].width).toBe(200 - CASCADE_PX);
    });

    it('leaves blocks on other lanes, or at other times, where they are', () => {
      const blocks = layout([comp('a', 'On A', ['A'], 600, 700), comp('b', 'On B', ['B'], 630, 700), comp('a2', 'On A later', ['A'], 700, 760)]);
      expect(blocks.map((b) => b.left)).toEqual([0, 200, 0]);
    });

    it('draws a block that starts with an earlier one at its real time, over the earlier one\'s right half', () => {
      const blocks = layout([comp('k', 'Kids Individual', ['A'], 600, 683), comp('l', 'Ladies Kata', ['A'], 600, 650)]);
      const top = (600 - 8 * 60) * PX_PER_MINUTE;
      // Both at their real start and end.
      expect(blocks.map((b) => [b.top, b.top + b.height])).toEqual([[top, top + 83], [top, top + 50]]);
      // The second covers the first's right half and is drawn on top (after it).
      expect(blocks.map((b) => b.comp.id)).toEqual(['k', 'l']);
      expect(blocks.map((b) => [b.left, b.width])).toEqual([[0, 200], [100, 100]]);
      // The first's text wraps in its uncovered left half.
      expect(blocks[0].coveredRight).toBe(100);
      expect(blocks[1].coveredRight).toBe(0);
    });

    it('steps a third block that starts with two others one step further in than the second', () => {
      const three = layout([comp('a', 'First', ['A'], 600, 800), comp('b', 'Second', ['A'], 600, 800), comp('c', 'Third', ['A'], 600, 800)]);
      const top = (600 - 8 * 60) * PX_PER_MINUTE;
      expect(three.map((b) => b.top)).toEqual([top, top, top]);
      expect(three.map((b) => b.left)).toEqual([0, 100, 100 + CASCADE_PX]);
      // Each keeps its text to the part the next one leaves in view.
      expect(three[0].coveredRight).toBe(100);
      expect(three[1].coveredRight).toBe(50);
      // On a phone's narrow lane it still overlaps at its real time, a quarter of the lane apart.
      const narrow = computeBlocksLayout({
        calendar: { competitions: [comp('a', 'First', ['A'], 600, 683), comp('b', 'Second', ['A'], 600, 650)] },
        visibleDays: [{ day: 1 }], courts: ['A'], laneWidth: 78, startHour: 8, endHour: 18,
      });
      expect(narrow.map((b) => [b.top, b.left])).toEqual([[top, 0], [top, 39]]);
    });

    it('keeps the earlier block\'s text clear of a later block that covers its right part', () => {
      const blocks = layout([comp('w', 'Wide', ['A', 'B'], 600, 700), comp('s', 'Later on B', ['B'], 630, 700)]);
      expect(blocks[1].left).toBe(200 + CASCADE_PX);
      expect(blocks[0].coveredRight).toBe(400 - (200 + CASCADE_PX));
    });

    it('steps a third clash further in, and keeps at least half the width', () => {
      const blocks = layout([comp('1', 'One', ['A'], 600, 900), comp('2', 'Two', ['A'], 670, 900), comp('3', 'Three', ['A'], 740, 900)]);
      expect(blocks.map((b) => b.left)).toEqual([0, CASCADE_PX, 2 * CASCADE_PX]);
      const deep = computeBlocksLayout({
        calendar: { competitions: Array.from({ length: 8 }, (_, i) => comp(String(i), `C${i}`, ['A'], 600 + i, 800)) },
        visibleDays: [{ day: 1 }], courts: ['A'], laneWidth: 76, startHour: 8, endHour: 18,
      });
      // A quarter of each block always stays in view.
      for (const b of deep) expect(b.width).toBeGreaterThanOrEqual(19);
      // Starting a minute apart, the second takes the right half of the first.
      expect(deep[1].left).toBe(38);
      // A narrow lane insets a later start by a quarter of the lane, not the full step.
      const narrow = computeBlocksLayout({
        calendar: { competitions: [comp('x', 'X', ['A'], 600, 900), comp('y', 'Y', ['A'], 700, 900)] },
        visibleDays: [{ day: 1 }], courts: ['A'], laneWidth: 76, startHour: 8, endHour: 18,
      });
      expect(narrow[1].left).toBe(19);
    });
  });

  describe('lane widths', () => {
    const calendar = { courts: ['A', 'B', 'C'], days: [{ day: 1, date: '16-05-2026' }, { day: 2, date: '17-05-2026' }], competitions: [] };
    it('keeps a phone\'s lanes at the plan\'s 76 px at least, and scrolls only when they do not fit', () => {
      const tight = calendarLayout({ calendar, width: 240, phone: true });
      expect(tight.laneWidth).toBe(PHONE_LANE_PX);
      expect(tight.lanesScrollSideways).toBe(true);
      const phone = calendarLayout({ calendar, width: 320, phone: true });
      expect(phone.laneWidth).toBeGreaterThanOrEqual(PHONE_LANE_PX);
      expect(phone.lanesScrollSideways).toBe(false);
    });
    it('shares the inside of the grid\'s frame, not the page\'s width, so lanes that fit do not scroll', () => {
      // A 375 px phone: 16 px page padding each side and the frame's 1 px border.
      const layoutAt375 = calendarLayout({ calendar, width: 375, frameWidth: 341, phone: true });
      expect(layoutAt375.laneWidth).toBe(Math.floor((341 - CLOCK_COL_PX) / 3));
      expect(CLOCK_COL_PX + layoutAt375.dayGridWidth).toBeLessThanOrEqual(341);
      expect(layoutAt375.lanesScrollSideways).toBe(false);
      // The page's width still decides the phone layout.
      expect(calendarLayout({ calendar, width: 1280, frameWidth: 1214 }).isPhone).toBe(false);
    });
  });

  describe('a raised block', () => {
    it('takes its full width back on hover or focus, over the block that covers it', () => {
      const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', 'css', 'styles.css'), 'utf8');
      const rule = css.match(/\.cal-block:hover,\s*\.cal-block:focus-within\s*\{([^}]*)\}/g) || [];
      const body = rule.join('\n');
      expect(body).toMatch(/--cal-covered:\s*0px\s*!important/);
            // The short block still lifts.
      expect(body).toMatch(/min-height:\s*52px/);
    });
    it('keeps the lines beside a raised block\'s Move button clear of it', () => {
      const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', 'css', 'styles.css'), 'utf8');
      const fine = css.match(/@media \(pointer: fine\)\s*\{([\s\S]*?)\n\}/);
      expect(fine).not.toBeNull();
      expect(fine[1]).toMatch(/\.cal-block--movable:hover \.cal-block__content::before/);
      expect(fine[1]).toMatch(/\.cal-block--movable:focus-within \.cal-block__content::before/);
      expect(fine[1]).toMatch(/float:\s*right/);
    });
  });

  describe('touchMoveFits', () => {
    it('fits a touch screen\'s Move beside the name from 46 px, and under it from 68 px', () => {
      expect(MOVE_ROW_PX).toBe(46);
      expect(NAME_ROW_PX + MOVE_ROW_PX).toBe(68);
      expect(touchMoveFits(45, false)).toBe(false);
      expect(touchMoveFits(46, false)).toBe(true);
      expect(touchMoveFits(67, true)).toBe(false);
      expect(touchMoveFits(68, true)).toBe(true);
    });
    it('draws every block at its real length on a touch screen: no minimum height for a movable block', () => {
      const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', 'css', 'styles.css'), 'utf8');
      const coarse = (css.match(/@media \(pointer: coarse\)\s*\{[\s\S]*?\n\}/g) || []).join('\n');
      const rules = coarse.match(/[^{}]*cal-block--movable[^{}]*\{[^}]*\}/g) || [];
      for (const r of rules) expect(r).not.toMatch(/min-height/);
      expect(coarse).toMatch(/\.cal-block--name-only:hover,\s*\.cal-block--name-only:focus-within\s*\{\s*min-height:\s*0;/);
    });
  });

  describe('pauseLabelLane', () => {
    it('puts a pause\'s name in the first lane no block covers during it', () => {
      const blocks = [{ startMins: 570, endMins: 900, minCourtIdx: 0, maxCourtIdx: 0 }];
      expect(pauseLabelLane(blocks, 3, 750, 810)).toBe(1);
      expect(pauseLabelLane(blocks, 3, 1000, 1060)).toBe(0);
      expect(pauseLabelLane([{ ...blocks[0], maxCourtIdx: 2 }], 3, 750, 810)).toBe(0);
    });
  });

  describe('fitBlockLines', () => {
    const line = (offsetTop, offsetHeight) => ({ offsetTop, offsetHeight, style: {} });
    it('clamps the line a block\'s foot cuts to the lines that fit, and leaves out the rest', () => {
      const name = line(6, 15);
      const time = line(23, 14);
      const warn = line(39, 42);
      const meta = line(83, 14);
      const content = { children: [name, time, warn, meta] };
      const open = { clientHeight: 80, querySelector: () => content };
      const root = { querySelectorAll: () => [open] };
      const saved = globalThis.getComputedStyle;
      globalThis.getComputedStyle = (el) => (el === open ? { paddingBottom: '6px' } : { lineHeight: '14px' });
      try {
        fitBlockLines(root);
      } finally {
        globalThis.getComputedStyle = saved;
      }
      expect(name.style).toEqual({});
      expect(time.style.display).toBe('');
      expect(warn.style.display).toBe('-webkit-box');
      expect(warn.style.webkitLineClamp).toBe('2');
      expect(meta.style.display).toBe('none');
    });
    it('keeps a name-only block to the whole name lines that fit, one at least, and none of its other lines', () => {
      const name = line(2, 30);
      const time = line(32, 14);
      const content = { children: [name, time] };
      const frame = { classList: { contains: (c) => c === 'cal-block--name-only' } };
      const open = { clientHeight: 34, querySelector: () => content, closest: () => frame };
      const saved = globalThis.getComputedStyle;
      globalThis.getComputedStyle = (el) => (el === open ? { paddingTop: '2px', paddingBottom: '2px' } : { lineHeight: '15px' });
      try {
        fitBlockLines({ querySelectorAll: () => [open] });
        expect(name.style.webkitLineClamp).toBe('2');
        expect(time.style.display).toBe('none');
        fitBlockLines({ querySelectorAll: () => [{ ...open, clientHeight: 10 }] });
        expect(name.style.webkitLineClamp).toBe('1');
      } finally {
        globalThis.getComputedStyle = saved;
      }
    });
    it('leaves a block that has no height yet alone', () => {
      const meta = line(83, 14);
      const content = { children: [line(6, 15), meta] };
      const root = { querySelectorAll: () => [{ clientHeight: 0, querySelector: () => content }] };
      fitBlockLines(root);
      expect(meta.style.display).toBe('');
      expect(() => fitBlockLines(null)).not.toThrow();
    });
  });

  describe('shiftLanes', () => {
    const allCourts = ['A', 'B', 'C', 'D'];

    it('shifts a two-shiaijo block to the right', () => {
      expect(shiftLanes(['A', 'B'], allCourts, 1)).toEqual(['B', 'C']);
      expect(shiftLanes(['A', 'B'], allCourts, 2)).toEqual(['C', 'D']);
    });

    it('clamps at the right edge', () => {
      expect(shiftLanes(['A', 'B'], allCourts, 5)).toEqual(['C', 'D']);
    });

    it('clamps at the left edge', () => {
      expect(shiftLanes(['C', 'D'], allCourts, -5)).toEqual(['A', 'B']);
    });
  });
});
