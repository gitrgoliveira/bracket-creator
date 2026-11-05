import { describe, it, expect } from 'vitest';
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
      expect(offScreen[0].label).toBe('Overlap on day 6, Shiaijo B');

      // With the tournament's dates the label names the day the way the rest
      // of the page does.
      const dated = detectOffScreenOverlaps({
        calendar: { ...calendar, days: [{ day: 6, date: '21-05-2026' }] },
        currentPageDays,
      });
      expect(dated[0].label).toBe('Overlap on Thu 21 May (day 6), Shiaijo B');
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
