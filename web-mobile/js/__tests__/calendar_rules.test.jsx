import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  parseClockTime,
  formatClockHHMM,
  pastMidnight,
  dayBreaks,
  replayCourt,
  overlaps,
} from '../calendar_rules.jsx';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const casesPath = resolve(__dirname, '../../../internal/engine/testdata/calendar_rule_cases.json');
const rawData = readFileSync(casesPath, 'utf8');
const cases = JSON.parse(rawData);

describe('dayBreaks (shared calendar_rule_cases.json)', () => {
  cases.dayBreaks.forEach((tc) => {
    it(tc.name, () => {
      const windows = dayBreaks(tc.pauses, tc.date, tc.days);
      expect(windows).toHaveLength(tc.expected.length);
      tc.expected.forEach((exp, i) => {
        expect(formatClockHHMM(windows[i].start)).toBe(exp.start);
        expect(formatClockHHMM(windows[i].end)).toBe(exp.end);
      });
    });
  });

  it('tolerates undefined pauses and days', () => {
    expect(dayBreaks(undefined, '01-07-2026', undefined)).toEqual([]);
  });

  it('uses day 1 when date is empty string', () => {
    const pauses = [
      { name: 'Day 1 Pause', start: '10:00', length: '30m', day: 1 },
      { name: 'Day 2 Pause', start: '11:00', length: '30m', day: 2 },
    ];
    const days = ['01-07-2026', '02-07-2026'];
    const windows = dayBreaks(pauses, '', days);
    expect(windows).toHaveLength(1);
    expect(formatClockHHMM(windows[0].start)).toBe('10:00');
    expect(formatClockHHMM(windows[0].end)).toBe('10:30');
  });
});

describe('replayCourt (shared calendar_rule_cases.json)', () => {
  cases.replayCourt.forEach((tc) => {
    it(tc.name, () => {
      const start = parseClockTime(tc.start);
      const breaks = tc.breaks.map((b) => ({
        start: parseClockTime(b.start),
        end: parseClockTime(b.end),
      }));
      const result = replayCourt(start, { runs: tc.runs, bufferPct: tc.bufferPct }, breaks);
      expect(Math.round(result.duration)).toBe(tc.expectedDurationMinutes);
      expect(formatClockHHMM(result.end)).toBe(tc.expectedEnd);
    });
  });

  it('returns end === start and duration 0 when recipe has no runs', () => {
    const start = 540;
    const res = replayCourt(start, { runs: [], bufferPct: 10 }, []);
    expect(res.end).toBe(start);
    expect(res.duration).toBe(0);
  });
});

describe('pastMidnight (shared calendar_rule_cases.json)', () => {
  cases.pastMidnight.forEach((tc) => {
    it(tc.name, () => {
      expect(pastMidnight(tc.endMins)).toBe(tc.expected);
    });
  });
});

describe('overlaps (shared calendar_rule_cases.json)', () => {
  cases.overlaps.forEach((tc) => {
    it(tc.name, () => {
      expect(overlaps(tc.a, tc.b)).toBe(tc.expected);
    });
  });
});

describe('parseClockTime additional cases', () => {
  it('accepts 9:30 and rejects invalid inputs', () => {
    expect(parseClockTime('9:30')).toBe(570);
    expect(parseClockTime(' 09:00')).toBe(null);
    expect(parseClockTime('24:00')).toBe(null);
    expect(parseClockTime('9:5')).toBe(null);
    expect(parseClockTime(null)).toBe(null);
  });
});

describe('formatClockHHMM additional cases', () => {
  it('formats unwrapped hours and clamps negative minutes to zero', () => {
    expect(formatClockHHMM(1470)).toBe('24:30');
    expect(formatClockHHMM(-5)).toBe('00:00');
  });
});
