// bc-cpdc: "Copy from previous match" moved from the footer (beside "Save
// lineup") to the header of each side's editor, matching the docs'
// walkthrough (docs/user-guide/organisers/team-tournaments.md: "use Copy
// from previous match at the top of the lineup panel"). The docs define
// behaviour, so the control moved rather than the sentence.
//
// This pins the ORDER an operator actually sees in the DOM: Copy above the
// position rows, Save below them. Asserting mere presence of the button
// would pass whether it sits in the header or the footer; only ordering
// (via Node.compareDocumentPosition, real DOM -- this suite mounts with
// real React via @testing-library/react, unlike the unit suite's vdom
// stub) catches a regression back to the old placement.

import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  resolveRoundIndex: () => 0,
  compMatches: () => [],
  AdminLineupHelpers: {
    positionsForSize: (n) => Array.from({ length: n }, (_, i) => ({ key: String(i + 1), label: String(i + 1) })),
    rosterFor: () => [],
    mergeRosterWithAssigned: (base) => (Array.isArray(base) ? base : []),
    teamIdOf: (t) => t?.id || t?.name || '',
    resolveMemberIdsForPositions: vi.fn().mockResolvedValue({ memberIds: {}, squad: [], failures: [] }),
    memberIdentityWarning: () => '',
  },
  API: {
    fetchMatchLineup: vi.fn().mockResolvedValue(null),
    fetchTeamLineup: vi.fn().mockResolvedValue(null),
    fetchSquads: vi.fn().mockResolvedValue({}),
    putMatchLineup: vi.fn().mockResolvedValue({ positions: {} }),
  },
};

let restoreGlobals;
let MatchLineupSideEditor;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ MatchLineupSideEditor } = await import('../../admin_schedule_lineup.jsx'));
});

afterAll(() => restoreGlobals());

const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };
const TEAM = { id: 'uuid-grouped', name: 'Grouped Team', number: 'T5' };
const MATCH = {
  id: 'match-1',
  compId: 'comp-1',
  sideA: { id: 'uuid-grouped', name: 'Grouped Team' },
  sideB: { id: 'other', name: 'Other' },
  status: 'scheduled',
};

describe('MatchLineupSideEditor places Copy above the positions and Save below them (bc-cpdc)', () => {
  it('renders Copy from previous match before the first position row, and Save lineup after it', async () => {
    let utils;
    await act(async () => {
      utils = render(
        <MatchLineupSideEditor
          comp={COMP}
          team={TEAM}
          match={MATCH}
          allMatches={[MATCH]}
          password="pw"
          showToast={vi.fn()}
        />
      );
    });
    const { container, findByRole } = utils;

    const saveBtn = await findByRole('button', { name: /Save lineup/ });
    const copyBtn = await findByRole('button', { name: /Copy from previous match/ });
    const firstPos = container.querySelector('[data-testid^="match-lineup-pos-"]');
    expect(firstPos).toBeTruthy();

    // Node.compareDocumentPosition: DOCUMENT_POSITION_FOLLOWING === 4.
    expect(copyBtn.compareDocumentPosition(firstPos) & 4).toBeTruthy();
    expect(firstPos.compareDocumentPosition(saveBtn) & 4).toBeTruthy();
  });
});
