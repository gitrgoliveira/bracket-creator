// Characterization of the Settings PUT body (cv-ui). The EXPECTED bodies below
// were captured by running a verbatim copy of AdminSettings.saveNow's
// derivation, as it stood before settingsPutBody existed, over this fixture
// grid. They are literal on purpose: the builder must reproduce today's body
// exactly, so a change to this file's expectations is a change to what
// Settings sends, and needs its own reason (the only planned one is the
// expectedParticipants key).
//
// The builder also carries the configVersion it is given; that key is checked
// on its own below, and stripped before the comparison, because it did not
// exist when the bodies were captured.
import { describe, it, expect } from 'vitest';
import { settingsPutBody } from '../competition_put_body.jsx';

const players = [{ id: 'p1', name: 'Aki Sato', dojo: 'Kodokan' }];
const indiv = {
  id: 'c1', name: "Men's Individual", date: '14-03-2026', startTime: '09:00',
  format: 'mixed', kind: 'individual', poolSize: 4, poolWinners: 2, poolSizeMode: 'min',
  courts: ['A', 'B'], roundRobin: false, withZekkenName: true, teamSize: 0, numberPrefix: 'M',
  poolFormat: '', poolMatchDurationSeconds: 180, knockoutMatchDurationSeconds: 240,
  swissRounds: 0, naginata: false, engi: false, checkInEnabled: false, leagueTiebreakTopN: 0,
  twoThirdPlaces: true, teamMatchType: '', extraQualifiers: '', status: 'setup',
  players, hasParticipantIDs: true, rosterVersion: 'r1', configVersion: 'v-base',
};
const team = {
  ...indiv, id: 'c2', name: 'Team Cup', kind: 'team', teamSize: 5, withZekkenName: false,
  numberPrefix: 'T', teamMatchType: 'fixed',
};

const FIXTURES = [
  { name: 'mixed individual, no edits', base: indiv, edits: {} },
  { name: 'mixed individual, start time edited', base: indiv, edits: { startTime: '15:00' } },
  { name: 'mixed individual to knockout', base: indiv, edits: { format: 'knockout' } },
  { name: 'knockout individual, name and prefix trimmed', base: { ...indiv, format: 'knockout', poolSize: 0, poolWinners: 0 }, edits: { name: '  Open Cup  ', numberPrefix: ' ab ' } },
  { name: 'mixed with zeroed pool fields', base: { ...indiv, poolSize: 0, poolWinners: 0 }, edits: {} },
  { name: 'mixed with larger-pools qualifiers set back to standard', base: { ...indiv, poolSizeMode: 'max', extraQualifiers: 'larger-pools' }, edits: { extraQualifiers: '' } },
  { name: 'league team with tiebreak', base: { ...team, format: 'league', poolSize: 0, poolWinners: 0, leagueTiebreakTopN: 2, poolFormat: 'single' }, edits: { leagueTiebreakTopN: NaN } },
  { name: 'swiss individual, swiss rounds box cleared', base: { ...indiv, format: 'swiss', poolSize: 0, poolWinners: 0, swissRounds: 5 }, edits: { swissRounds: NaN } },
  { name: 'swiss individual, swiss rounds edited', base: { ...indiv, format: 'swiss', poolSize: 0, poolWinners: 0, swissRounds: 5 }, edits: { swissRounds: 6 } },
  { name: 'cleared durations', base: indiv, edits: { poolMatchDurationSeconds: 0, knockoutMatchDurationSeconds: NaN } },
  { name: 'engi individual, check-in on', base: { ...indiv, engi: true, format: 'knockout', poolSize: 0, poolWinners: 0 }, edits: { checkInEnabled: true } },
  { name: 'kachinuki team, team size cleared, two third places off', base: { ...team, teamMatchType: 'kachinuki', teamSize: 3 }, edits: { teamSize: NaN, twoThirdPlaces: false } },
  { name: 'individual to team', base: { ...indiv, engi: true }, edits: { kind: 'team' } },
  { name: 'team to individual', base: { ...team, teamMatchType: 'kachinuki' }, edits: { kind: 'individual' } },
  { name: 'legacy naginata record with no twoThirdPlaces and no date', base: (() => { const b = { ...indiv, naginata: true, date: '' }; delete b.twoThirdPlaces; return b; })(), edits: { courts: ['C'] } },
  { name: 'legacy record with no twoThirdPlaces and no durations', base: (() => { const b = { ...indiv }; delete b.twoThirdPlaces; delete b.poolMatchDurationSeconds; delete b.knockoutMatchDurationSeconds; delete b.swissRounds; delete b.leagueTiebreakTopN; delete b.teamMatchType; delete b.extraQualifiers; delete b.poolFormat; return b; })(), edits: { roundRobin: true } },
  { name: 'date typed with slashes', base: indiv, edits: { date: '15/03/2026' } },
  { name: 'invalid date', base: indiv, edits: { date: '31-02-2026' } },
  { name: 'empty name', base: indiv, edits: { name: '   ' } },
];

const EXPECTED = {
  "mixed individual, no edits": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "mixed",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "mixed individual, start time edited": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "15:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "mixed",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "mixed individual to knockout": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 0,
      "poolWinners": 0,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "knockout",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "knockout individual, name and prefix trimmed": {
    "body": {
      "id": "c1",
      "name": "Open Cup",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 0,
      "poolWinners": 0,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "ab",
      "format": "knockout",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "mixed with zeroed pool fields": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 0,
      "poolWinners": 0,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "mixed",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "mixed with larger-pools qualifiers set back to standard": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "max",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "mixed",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "league team with tiebreak": {
    "body": {
      "id": "c2",
      "name": "Team Cup",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 0,
      "poolWinners": 0,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": false,
      "teamSize": 5,
      "numberPrefix": "T",
      "format": "league",
      "kind": "team",
      "poolFormat": "single",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 2,
      "twoThirdPlaces": true,
      "teamMatchType": "fixed",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "swiss individual, swiss rounds box cleared": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 0,
      "poolWinners": 0,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "swiss",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 5,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "swiss individual, swiss rounds edited": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 0,
      "poolWinners": 0,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "swiss",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 6,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "cleared durations": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "mixed",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 0,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "engi individual, check-in on": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 0,
      "poolWinners": 0,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "knockout",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": true,
      "checkInEnabled": true,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "kachinuki team, team size cleared, two third places off": {
    "body": {
      "id": "c2",
      "name": "Team Cup",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": false,
      "teamSize": 3,
      "numberPrefix": "T",
      "format": "mixed",
      "kind": "team",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": false,
      "teamMatchType": "kachinuki",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "individual to team": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": false,
      "teamSize": 5,
      "numberPrefix": "M",
      "format": "mixed",
      "kind": "team",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "fixed",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "team to individual": {
    "body": {
      "id": "c2",
      "name": "Team Cup",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": false,
      "teamSize": 0,
      "numberPrefix": "T",
      "format": "mixed",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "fixed",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "legacy naginata record with no twoThirdPlaces and no date": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "",
      "startTime": "09:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "min",
      "courts": [
        "C"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "mixed",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": true,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": false,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "legacy record with no twoThirdPlaces and no durations": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "14-03-2026",
      "startTime": "09:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": true,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "mixed",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 0,
      "knockoutMatchDurationSeconds": 0,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "date typed with slashes": {
    "body": {
      "id": "c1",
      "name": "Men's Individual",
      "date": "15-03-2026",
      "startTime": "09:00",
      "poolSize": 4,
      "poolWinners": 2,
      "poolSizeMode": "min",
      "courts": [
        "A",
        "B"
      ],
      "roundRobin": false,
      "withZekkenName": true,
      "teamSize": 0,
      "numberPrefix": "M",
      "format": "mixed",
      "kind": "individual",
      "poolFormat": "",
      "poolMatchDurationSeconds": 180,
      "knockoutMatchDurationSeconds": 240,
      "swissRounds": 0,
      "naginata": false,
      "engi": false,
      "checkInEnabled": false,
      "leagueTiebreakTopN": 0,
      "twoThirdPlaces": true,
      "teamMatchType": "",
      "extraQualifiers": "",
      "expectedParticipants": 0
    }
  },
  "invalid date": {
    "error": "Invalid date. Please pick a valid day."
  },
  "empty name": {
    "error": "Competition name is required."
  }
}
;

describe('settingsPutBody reproduces the Settings PUT body captured before the hoist', () => {
  it('covers every fixture', () => {
    expect(FIXTURES.map(f => f.name)).toEqual(Object.keys(EXPECTED));
  });
  for (const f of FIXTURES) {
    it(f.name, () => {
      const got = settingsPutBody(f.base, f.edits, 'v-given');
      const want = EXPECTED[f.name];
      if (want.error) {
        expect(got).toEqual({ error: want.error });
        return;
      }
      const { configVersion, ...rest } = got.body;
      expect(configVersion).toBe('v-given');
      expect(rest).toEqual(want.body);
    });
  }
});
