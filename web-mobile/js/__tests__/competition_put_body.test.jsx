import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  settingsPutBody,
  describeSettingsEdits,
  expectedParticipantsHint,
  mergeSavedCompetition,
  MAX_EXPECTED_PARTICIPANTS,
} from '../competition_put_body.jsx';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

describe('settingsPutBody', () => {
  const base = {
    id: 'c1',
    name: "Men's Individual",
    date: '14-03-2026',
    startTime: '09:00',
    format: 'mixed',
    kind: 'individual',
    poolSize: 4,
    poolWinners: 2,
    poolSizeMode: 'min',
    courts: ['A', 'B'],
    roundRobin: false,
    withZekkenName: true,
    teamSize: 0,
    numberPrefix: 'M',
    poolFormat: '',
    poolMatchDurationSeconds: 180,
    knockoutMatchDurationSeconds: 240,
    swissRounds: 0,
    naginata: false,
    engi: false,
    checkInEnabled: false,
    leagueTiebreakTopN: 0,
    twoThirdPlaces: true,
    teamMatchType: '',
    extraQualifiers: '',
    status: 'setup',
    players: [{ id: 'p1', name: 'Aki Sato', dojo: 'Kodokan' }],
    rosterVersion: 'r1',
    configVersion: 'old',
  };

  it('sends the version it is given and never reads base.configVersion', () => {
    const res = settingsPutBody(base, {}, 'new');
    expect(res.error).toBeUndefined();
    expect(res.body.configVersion).toBe('new');
  });

  it('never sends players, status, or rosterVersion', () => {
    const res = settingsPutBody(base, {}, 'new');
    expect(res.body.players).toBeUndefined();
    expect(res.body.status).toBeUndefined();
    expect(res.body.rosterVersion).toBeUndefined();
  });

  it('edits of { date, startTime, courts } alone keep every other field from base', () => {
    const edits = {
      date: '15-03-2026',
      startTime: '11:00',
      courts: ['C', 'D'],
    };
    const res = settingsPutBody(base, edits, 'v-updated');
    expect(res.error).toBeUndefined();
    expect(res.body.date).toBe('15-03-2026');
    expect(res.body.startTime).toBe('11:00');
    expect(res.body.courts).toEqual(['C', 'D']);
    expect(res.body.configVersion).toBe('v-updated');

    // Kept from base
    expect(res.body.id).toBe(base.id);
    expect(res.body.name).toBe(base.name);
    expect(res.body.format).toBe(base.format);
    expect(res.body.kind).toBe(base.kind);
    expect(res.body.poolSize).toBe(base.poolSize);
    expect(res.body.poolWinners).toBe(base.poolWinners);
    expect(res.body.poolSizeMode).toBe(base.poolSizeMode);
    expect(res.body.roundRobin).toBe(base.roundRobin);
    expect(res.body.withZekkenName).toBe(base.withZekkenName);
    expect(res.body.teamSize).toBe(base.teamSize);
    expect(res.body.numberPrefix).toBe(base.numberPrefix);
    expect(res.body.poolMatchDurationSeconds).toBe(base.poolMatchDurationSeconds);
    expect(res.body.knockoutMatchDurationSeconds).toBe(base.knockoutMatchDurationSeconds);
    expect(res.body.swissRounds).toBe(base.swissRounds);
    expect(res.body.naginata).toBe(base.naginata);
    expect(res.body.engi).toBe(base.engi);
    expect(res.body.checkInEnabled).toBe(base.checkInEnabled);
    expect(res.body.leagueTiebreakTopN).toBe(base.leagueTiebreakTopN);
    expect(res.body.twoThirdPlaces).toBe(base.twoThirdPlaces);
    expect(res.body.expectedParticipants).toBe(0);
  });

  it('an edit of expectedParticipants appears in the body with the given version', () => {
    const edits = { expectedParticipants: 40 };
    const res = settingsPutBody(base, edits, 'v-updated');
    expect(res.error).toBeUndefined();
    expect(res.body.expectedParticipants).toBe(40);
    expect(res.body.configVersion).toBe('v-updated');
  });

  it('clearing expectedParticipants (empty string / NaN edit) sends 0', () => {
    const resEmpty = settingsPutBody({ ...base, expectedParticipants: 40 }, { expectedParticipants: '' }, 'v-ver');
    expect(resEmpty.body.expectedParticipants).toBe(0);

    const resNaN = settingsPutBody({ ...base, expectedParticipants: 40 }, { expectedParticipants: NaN }, 'v-ver');
    expect(resNaN.body.expectedParticipants).toBe(0);
  });

  it('a base value of expectedParticipants with no edit is carried', () => {
    const res = settingsPutBody({ ...base, expectedParticipants: 50 }, {}, 'v-ver');
    expect(res.body.expectedParticipants).toBe(50);
  });

  it('an edit of expectedParticipants never introduces a players key', () => {
    const res = settingsPutBody(base, { expectedParticipants: 40 }, 'v-ver');
    expect(res.body.players).toBeUndefined();
  });
});

describe('describeSettingsEdits', () => {
  it('returns empty string for empty edits', () => {
    expect(describeSettingsEdits({})).toBe('');
    expect(describeSettingsEdits(null)).toBe('');
  });

  it('formats single field', () => {
    expect(describeSettingsEdits({ startTime: '15:00' })).toBe('Start time to 15:00.');
  });

  it('formats several fields', () => {
    expect(describeSettingsEdits({ startTime: '15:00', date: '15-03-2026' }))
      .toBe('Start time to 15:00, Date to 15-03-2026.');
  });

  it('formats boolean fields as on and off', () => {
    expect(describeSettingsEdits({ withZekkenName: true }))
      .toBe('Use Zekken display name to on.');
    expect(describeSettingsEdits({ naginata: false }))
      .toBe('Naginata competition to off.');
  });

  it('formats array fields joined with comma and space', () => {
    expect(describeSettingsEdits({ courts: ['A', 'B'] }))
      .toBe('Assigned shiaijo (courts) to A, B.');
  });

  it('formats blank/cleared values as blank', () => {
    expect(describeSettingsEdits({ name: '' })).toBe('Name to blank.');
    expect(describeSettingsEdits({ date: '' })).toBe('Date to blank.');
    expect(describeSettingsEdits({ teamSize: NaN })).toBe('Team size to blank.');
    expect(describeSettingsEdits({ courts: [] })).toBe('Assigned shiaijo (courts) to blank.');
  });

  it('formats duration fields as m:ss', () => {
    expect(describeSettingsEdits({ poolMatchDurationSeconds: 180 }))
      .toBe('Pool match duration to 3:00.');
    expect(describeSettingsEdits({ knockoutMatchDurationSeconds: 150 }))
      .toBe('Knockout match duration to 2:30.');
  });

  it('formats enum fields using their option labels', () => {
    expect(describeSettingsEdits({ format: 'knockout' }))
      .toBe('Format to Knockout only.');
    expect(describeSettingsEdits({ kind: 'team' }))
      .toBe('Competition type to Team.');
    expect(describeSettingsEdits({ poolSizeMode: 'min' }))
      .toBe('Pool size is a to minimum.');
    expect(describeSettingsEdits({ teamMatchType: 'kachinuki' }))
      .toBe('Team match format to Kachinuki (winner stays on).');
    expect(describeSettingsEdits({ extraQualifiers: 'larger-pools' }))
      .toBe('Knockout qualifiers to Oversized send +1.');
  });

  it('formats expectedParticipants label', () => {
    expect(describeSettingsEdits({ expectedParticipants: 40 }))
      .toBe('Expected participants to 40.');
    expect(describeSettingsEdits({ expectedParticipants: NaN }))
      .toBe('Expected participants to blank.');
  });
});

describe('expectedParticipantsHint', () => {
  it('returns standing guidance when empty or 0 in setup', () => {
    const want = 'How many you expect. Estimates use this number until more register. Kept with the competition.';
    expect(expectedParticipantsHint(0, { status: 'setup' })).toBe(want);
    expect(expectedParticipantsHint(NaN, { status: 'setup' })).toBe(want);
    expect(expectedParticipantsHint(undefined, { status: 'setup' })).toBe(want);
    expect(expectedParticipantsHint(0, { status: 'setup', isTeam: true })).toBe(want);
  });

  it('returns zero-registered message when N is set and count C is 0', () => {
    expect(expectedParticipantsHint(40, { status: 'setup', rosterCount: 0 }))
      .toBe('Estimates use 40 until more than 40 register. 0 registered so far.');
  });

  it('returns check-in variant when N is set, check-in applies, and C (checked in) is 0', () => {
    expect(expectedParticipantsHint(40, { status: 'setup', rosterCount: 12, checkedInCount: 0, checkInApplies: true }))
      .toBe('Estimates use 40 until more than 40 check in. 12 registered, 0 checked in so far.');
  });

  it('returns fewer-than-expected message when 0 < C < N', () => {
    expect(expectedParticipantsHint(40, { status: 'setup', rosterCount: 12 }))
      .toBe('12 registered, fewer than the 40 you expected. Estimates still use 40; lower this number to use the registrations.');
  });

  it('returns check-in variant when 0 < C < N with check-in applying', () => {
    expect(expectedParticipantsHint(40, { status: 'setup', rosterCount: 20, checkedInCount: 12, checkInApplies: true }))
      .toBe('12 checked in, fewer than the 40 you expected. Estimates still use 40; lower this number to use the check-ins.');
  });

  it('returns matches-expected message when C === N', () => {
    expect(expectedParticipantsHint(40, { status: 'setup', rosterCount: 40 }))
      .toBe('Registrations match the 40 you expected.');
    expect(expectedParticipantsHint(40, { status: 'setup', rosterCount: 45, checkedInCount: 40, checkInApplies: true }))
      .toBe('Check-ins match the 40 you expected.');
  });

  it('returns more-than-expected message when C > N', () => {
    expect(expectedParticipantsHint(40, { status: 'setup', rosterCount: 45 }))
      .toBe('Estimates use the 45 registered, more than the 40 you expected.');
  });

  it('returns check-in variant when C > N with check-in applying', () => {
    expect(expectedParticipantsHint(40, { status: 'setup', rosterCount: 45, checkedInCount: 42, checkInApplies: true }))
      .toBe('Estimates use the 42 checked in, more than the 40 you expected.');
  });

  it('returns after-the-draw line naming drawn count when past setup', () => {
    expect(expectedParticipantsHint(40, { status: 'draw-ready', rosterCount: 16 }))
      .toBe('The draw has been made, so estimates use the 16 participants in it.');
    expect(expectedParticipantsHint(40, { status: 'draw-ready', isTeam: true, rosterCount: 8 }))
      .toBe('The draw has been made, so estimates use the 8 teams in it.');
    expect(expectedParticipantsHint(40, { status: 'draw-ready', isTeam: false, rosterCount: 1 }))
      .toBe('The draw has been made, so estimates use the 1 participant in it.');
    expect(expectedParticipantsHint(40, { status: 'draw-ready', isTeam: true, rosterCount: 1 }))
      .toBe('The draw has been made, so estimates use the 1 team in it.');
  });
});

describe('mergeSavedCompetition', () => {
  const base = {
    id: 'c1',
    name: "Men's Individual",
    date: '14-03-2026',
    startTime: '09:00',
    format: 'mixed',
    kind: 'individual',
    poolSize: 4,
    poolWinners: 2,
    poolSizeMode: 'min',
    courts: ['A', 'B'],
    roundRobin: false,
    withZekkenName: true,
    teamSize: 0,
    numberPrefix: 'M',
    poolFormat: '',
    poolMatchDurationSeconds: 180,
    knockoutMatchDurationSeconds: 240,
    swissRounds: 0,
    naginata: false,
    engi: false,
    checkInEnabled: false,
    leagueTiebreakTopN: 0,
    twoThirdPlaces: true,
    teamMatchType: '',
    extraQualifiers: '',
    status: 'setup',
    players: [{ id: 'p1', name: 'Aki Sato', dojo: 'Kodokan' }],
    rosterVersion: 'r1',
    configVersion: 'old',
    expectedParticipants: 40,
  };

  it('a save that cleared the expected count does not resend the old count', () => {
    const held = {
      ...base,
      expectedParticipants: 40,
    };
    const sent = { expectedParticipants: 0 };
    const answerWithoutKey = {
      id: 'c1',
      name: "Men's Individual",
      configVersion: 'v2',
    };
    const merged = mergeSavedCompetition(held, answerWithoutKey, sent);
    expect(merged.expectedParticipants).toBe(0);

    const nextBody = settingsPutBody(merged, {}, 'v3');
    expect(nextBody.body.expectedParticipants).toBe(0);
  });

  it('an answer without a configVersion keeps the held version, so the next save stays checked', () => {
    const held = {
      ...base,
      configVersion: 'v-old',
    };
    for (const answerWithoutVersion of [
      { id: 'c1', name: "Men's Individual" },
      { id: 'c1', name: "Men's Individual", configVersion: '' },
    ]) {
      const merged = mergeSavedCompetition(held, answerWithoutVersion);
      expect(merged.configVersion).toBe('v-old');
      expect(settingsPutBody(merged, {}, merged.configVersion).body.configVersion).toBe('v-old');
    }
  });

  it('clearing with empty string, null, or NaN sets expectedParticipants to 0', () => {
    const held = { ...base, expectedParticipants: 40 };
    const answerWithoutKey = { id: 'c1', name: "Men's Individual" };

    for (const val of ['', null, NaN]) {
      const merged = mergeSavedCompetition(held, answerWithoutKey, { expectedParticipants: val });
      expect(merged.expectedParticipants).toBe(0);
    }
  });

  it('keeps held expectedParticipants when sent did not clear it or sent is omitted', () => {
    const held = { ...base, expectedParticipants: 40 };
    const answerWithoutKey = { id: 'c1', name: "Men's Individual" };

    const mergedNoSent = mergeSavedCompetition(held, answerWithoutKey);
    expect(mergedNoSent.expectedParticipants).toBe(40);

    const mergedSentOther = mergeSavedCompetition(held, answerWithoutKey, { name: "New Name" });
    expect(mergedSentOther.expectedParticipants).toBe(40);

    const mergedSentValue = mergeSavedCompetition(held, answerWithoutKey, { expectedParticipants: 50 });
    expect(mergedSentValue.expectedParticipants).toBe(40);
  });
});

describe('MAX_EXPECTED_PARTICIPANTS constant and source gates', () => {
  it('is a positive integer', () => {
    expect(Number.isInteger(MAX_EXPECTED_PARTICIPANTS)).toBe(true);
    expect(MAX_EXPECTED_PARTICIPANTS).toBeGreaterThan(0);
    expect(MAX_EXPECTED_PARTICIPANTS).toBe(1024);
  });

  it('admin_setup.jsx and admin_competition_settings.jsx contain no bare 1024 literal for expected participants cap', () => {
    const setupSrc = readFileSync(resolve(__dirname, '../admin_setup.jsx'), 'utf8');
    const settingsSrc = readFileSync(resolve(__dirname, '../admin_competition_settings.jsx'), 'utf8');

    expect(setupSrc).not.toMatch(/\b1024\b/);
    expect(settingsSrc).not.toMatch(/\b1024\b/);
  });
});
