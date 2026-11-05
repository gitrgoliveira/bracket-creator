import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  blockMinutes,
  parseClock,
  formatClock,
  computePauseRowStatus,
} from '../pause_list.jsx';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

describe('blockMinutes (shared duration cases JSON table)', () => {
  const casesPath = resolve(__dirname, '../../../internal/engine/testdata/block_duration_cases.json');
  const rawData = readFileSync(casesPath, 'utf8');
  const { cases } = JSON.parse(rawData);

  it('matches every test case in block_duration_cases.json exactly', () => {
    cases.forEach(({ input, minutes }) => {
      const actual = blockMinutes(input);
      expect(actual, `Input: ${JSON.stringify(input)}`).toBe(minutes);
    });
  });

  it('returns 0 for non-string types or undefined/null', () => {
    expect(blockMinutes(null)).toBe(0);
    expect(blockMinutes(undefined)).toBe(0);
    expect(blockMinutes(123)).toBe(0);
    expect(blockMinutes({})).toBe(0);
  });
});

describe('parseClock and formatClock', () => {
  it('parses valid 24h clock strings into minutes since midnight', () => {
    expect(parseClock('00:00')).toBe(0);
    expect(parseClock('09:30')).toBe(570);
    expect(parseClock('12:00')).toBe(720);
    expect(parseClock('23:59')).toBe(1439);
    expect(parseClock('9:15')).toBe(555);
  });

  it('returns null for invalid clock strings', () => {
    expect(parseClock('')).toBe(null);
    expect(parseClock('24:00')).toBe(null);
    expect(parseClock('12:60')).toBe(null);
    expect(parseClock('abc')).toBe(null);
    expect(parseClock(null)).toBe(null);
  });

  it('formats minutes since midnight into HH:MM', () => {
    expect(formatClock(0)).toBe('00:00');
    expect(formatClock(570)).toBe('09:30');
    expect(formatClock(720)).toBe('12:00');
    expect(formatClock(1439)).toBe('23:59');
  });
});

describe('computePauseRowStatus', () => {
  describe('usable pause rows', () => {
    it('computes "Matches stop HH:MM to HH:MM." for valid start and duration', () => {
      const pauses = [{ name: 'Lunch', start: '12:00', length: '1h', day: 0 }];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 1);
      expect(status.text).toBe('Matches stop 12:00 to 13:00.');
      expect(status.warn).toBe(false);
    });

    it('identifies end times that pass midnight', () => {
      const pauses = [{ name: 'Late Break', start: '23:30', length: '1h', day: 1 }];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 1);
      expect(status.text).toBe('Matches stop 23:30 to 00:30 (after midnight).');
      expect(status.warn).toBe(false);
    });

    it('does not call a pause ending at 24:00 after midnight', () => {
      const atMidnight = [{ name: 'Late Break', start: '23:00', length: '1h', day: 1 }];
      const statusAt = computePauseRowStatus(atMidnight[0], 0, atMidnight, 1);
      expect(statusAt.text).toBe('Matches stop 23:00 to 00:00.');
      expect(statusAt.warn).toBe(false);

      const pastMidnight = [{ name: 'Late Break', start: '23:01', length: '1h', day: 1 }];
      const statusPast = computePauseRowStatus(pastMidnight[0], 0, pastMidnight, 1);
      expect(statusPast.text).toBe('Matches stop 23:01 to 00:01 (after midnight).');
      expect(statusPast.warn).toBe(false);
    });
  });

  describe('incomplete pause rows', () => {
    it('shows exact sentence when start time is missing', () => {
      const pauses = [{ name: 'Opening', start: '', length: '30m', day: 1 }];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 1);
      expect(status.text).toBe('Not used yet: give it a start time.');
      expect(status.warn).toBe(true);
    });

    it('shows missing length message when length is missing or unparseable', () => {
      const pauses = [{ name: 'Lunch', start: '12:00', length: '', day: 1 }];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 1);
      expect(status.text).toBe('Not used yet: give it a length like 30m.');
      expect(status.warn).toBe(true);
    });

    it('prioritizes missing start over missing length', () => {
      const pauses = [{ name: 'TBD', start: '', length: '', day: 1 }];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 1);
      expect(status.text).toBe('Not used yet: give it a start time.');
      expect(status.warn).toBe(true);
    });
  });

  describe('day out of range', () => {
    it('warns when day is greater than tournament durationDays', () => {
      const pauses = [{ name: 'Closing', start: '17:00', length: '45m', day: 3 }];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 2);
      expect(status.text).toBe('Not used: the tournament no longer has Day 3.');
      expect(status.warn).toBe(true);
    });
  });

  describe('overlapping pauses', () => {
    it('detects overlap on multi-day tournament and formats "Together with <Name>: matches stop ... on Day N."', () => {
      const pauses = [
        { name: 'Lunch', start: '12:30', length: '1h', day: 2 },
        { name: 'Ceremony', start: '13:00', length: '45m', day: 2 },
      ];
      const status0 = computePauseRowStatus(pauses[0], 0, pauses, 2);
      expect(status0.text).toBe('Together with Ceremony: matches stop 12:30 to 13:45 on Day 2.');

      const status1 = computePauseRowStatus(pauses[1], 1, pauses, 2);
      expect(status1.text).toBe('Together with Lunch: matches stop 12:30 to 13:45 on Day 2.');
    });

    it('omits "on Day N" when tournament is one day', () => {
      const pauses = [
        { name: 'Lunch', start: '12:00', length: '1h', day: 1 },
        { name: 'Meeting', start: '12:30', length: '45m', day: 1 },
      ];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 1);
      expect(status.text).toBe('Together with Meeting: matches stop 12:00 to 13:15.');
    });

    it('omits "on Day N" when both pauses are Every Day (day 0)', () => {
      const pauses = [
        { name: 'Lunch', start: '12:00', length: '1h', day: 0 },
        { name: 'Break', start: '12:45', length: '30m', day: 0 },
      ];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 3);
      expect(status.text).toBe('Together with Break: matches stop 12:00 to 13:15.');
    });

    it('uses fallback name "another pause" when the overlapping pause has blank name', () => {
      const pauses = [
        { name: 'Lunch', start: '12:00', length: '1h', day: 0 },
        { name: '   ', start: '12:30', length: '30m', day: 0 },
      ];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 1);
      expect(status.text).toBe('Together with another pause: matches stop 12:00 to 13:00.');
    });

    it('detects day 0 overlapping with day N on Day N in a multi-day tournament', () => {
      const pauses = [
        { name: 'Daily Meeting', start: '12:00', length: '1h', day: 0 },
        { name: 'Day 2 Special', start: '12:30', length: '45m', day: 2 },
      ];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 3);
      expect(status.text).toBe('Together with Day 2 Special: matches stop 12:00 to 13:15 on Day 2.');
    });

    it('does not report overlap if pauses are on different specific days', () => {
      const pauses = [
        { name: 'Day 1 Pause', start: '12:00', length: '1h', day: 1 },
        { name: 'Day 2 Pause', start: '12:00', length: '1h', day: 2 },
      ];
      const status = computePauseRowStatus(pauses[0], 0, pauses, 2);
      expect(status.text).toBe('Matches stop 12:00 to 13:00.');
    });
  });
});
