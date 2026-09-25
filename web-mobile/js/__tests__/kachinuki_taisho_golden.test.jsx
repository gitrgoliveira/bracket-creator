import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { kachinukiTaishoPairing } from '../lineup_resolver.jsx';

// JS half of the shared Go/JS table for the kachinuki encho rule (bc-kten):
// only the last bout, taisho against taisho, may go to encho. Go half:
// TestKachinukiTaishoPairingGolden (internal/domain). The editor's Encho
// button asks kachinukiTaishoPairing and the server refuses with
// domain.KachinukiTaishoPairing, so the two must agree on who a team's
// taisho is, including a vacancy at the back, a slot picked by number and
// not yet named, and a 5-person lineup saved under numeric keys.
describe('kachinuki taisho pairing: the shared Go/JS table', () => {
  const table = JSON.parse(
    readFileSync(
      resolve(__dirname, '..', '..', '..', 'internal', 'domain', 'testdata', 'kachinuki_taisho.json'),
      'utf8'
    )
  );

  it('the shared table is present and non-empty', () => {
    expect(
      table.cases?.length,
      'internal/domain/testdata/kachinuki_taisho.json parsed to zero cases: the mirror would assert nothing'
    ).toBeGreaterThan(0);
  });

  it.each(table.cases.map((c) => [c.name, c]))('%s', (_name, c) => {
    const got = kachinukiTaishoPairing({ teamSize: c.teamSize, lineupA: c.lineupA, lineupB: c.lineupB, a: c.a, b: c.b });
    expect(got).toEqual({ taisho: c.taisho, known: c.known });
  });
});
