import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { maskSnapshotForBridge, maskPatchForBridge } from '../court_bridge.jsx';

// The server's maskView.sideReading reads the same table
// (TestMaskViewSideReading_SharedTable), so the bridge's copy of the mask
// cannot drift from it.
const casesPath = resolve(__dirname, '../../../internal/mobileapp/testdata/public_names_side_cases.json');
const { roster, cases } = JSON.parse(readFileSync(casesPath, 'utf8'));

function competition() {
    const players = roster.map((r) => ({ id: r.id, name: r.name, number: r.number || undefined, dojo: 'Dojo' }));
    return { id: 'c1', name: 'Open', publicNames: 'hidden', config: { players } };
}

describe('the court bridge mask reads a side as maskView.sideReading does (shared public_names_side_cases.json)', () => {
    for (const c of cases) {
        it(`${c.name} (patch)`, () => {
            const patch = { result: { id: 'M1', sideA: c.side, sideAId: c.id } };
            expect(maskPatchForBridge(patch, competition()).result.sideA).toBe(c.reading);
        });

        it(`${c.name} (snapshot)`, () => {
            const comp = { ...competition(), bracket: { rounds: [[{ id: 'M1', sideA: c.side, sideAId: c.id }]] } };
            const [out] = maskSnapshotForBridge([comp]);
            expect(out.bracket.rounds[0][0].sideA).toBe(c.reading);
        });
    }
});
