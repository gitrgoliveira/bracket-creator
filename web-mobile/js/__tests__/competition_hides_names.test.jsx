import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { competitionHidesNames, competitionCallsByNumber } from '../competition_shape.jsx';

// The server's publicNames.hides reads the same table
// (TestPublicNamesHides_SharedTable), so the mirror cannot drift from it.
const casesPath = resolve(__dirname, '../../../internal/mobileapp/testdata/public_names_hides_cases.json');
const { cases } = JSON.parse(readFileSync(casesPath, 'utf8'));

describe('competitionHidesNames (shared public_names_hides_cases.json)', () => {
    for (const c of cases) {
        it(c.name, () => {
            const comp = { publicNames: c.publicNames, kind: c.kind || 'individual', teamSize: c.teamSize || 0 };
            expect(competitionHidesNames({ mode: c.mode }, comp)).toBe(c.hides);
        });
    }

    it('reads a competition with no tournament as an officiated one', () => {
        expect(competitionHidesNames(null, { publicNames: 'person' })).toBe(true);
        expect(competitionHidesNames(null, null)).toBe(false);
    });
});

describe('competitionCallsByNumber', () => {
    it('calls an individual competition that hides its names by number', () => {
        expect(competitionCallsByNumber(null, { publicNames: 'hidden' })).toBe(true);
    });

    it('keeps team names in the call of a team competition', () => {
        expect(competitionCallsByNumber(null, { publicNames: 'hidden', kind: 'team' })).toBe(false);
        expect(competitionCallsByNumber(null, { publicNames: 'hidden', teamSize: 3 })).toBe(false);
    });

    it('names no one by number in a self-run tournament', () => {
        expect(competitionCallsByNumber({ mode: 'self-run' }, { publicNames: 'hidden' })).toBe(false);
    });
});
