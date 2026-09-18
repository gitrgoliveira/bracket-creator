// bc-dnst: a fixed-order bout row records no fighter name, not the team's.
//
// Operator ruling 2026-09-17: the storage is the source of truth, so a row
// must not record a value that is not what it claims to be. A fixed-order
// bout settles at the MATCH level, so the row knows no per-fighter identity
// when no lineup names one. It used to write the TEAM's own name into
// sideA/sideB anyway, which every display then had to filter back out; the
// ones that did not showed the team's name where the competitor goes.
//
// Nothing needed that copy: attribution reads the match-level name first
// (state.SubBoutWinnerSide, and the JS IV fallback mirrors it), and the
// server's own quick-score path already writes this exact shape.
//
// THE DAIHYOSEN ROW IS THE EXCEPTION and keeps the team names, because the
// hantei mark is placed on the winner's side by comparing the winner against
// that row's own sideA/sideB. Blanking them dropped the mark from the wire.
// That is pinned here too, so the exception cannot be "tidied" away.

import React from 'react';
import { render, act, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const originals = {};
let ScoreEditorModal;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

beforeEach(() => { window.API.recordScore.mockClear(); });

function makeMatch(overrides = {}) {
  return {
    id: 'm-pool-1',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    sideA: { id: 'team-kyoto', name: 'Kyoto' },
    sideB: { id: 'team-osaka', name: 'Osaka' },
    ...overrides,
  };
}

async function finish() {
  await act(async () => { fireEvent.click(screen.getByText('Finish')); });
  await act(async () => { fireEvent.click(screen.getByText('Tap again to finish')); });
}

async function scoreAndCapture(match) {
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={(p) => window.API.recordScore('comp1', match.id, p, '', match)} password="" />);
  });
  // One strike on the first bout, so the encounter has something to save.
  const ippons = document.querySelectorAll('button.ipt-btn');
  await act(async () => { fireEvent.click(ippons[0]); });
  await finish();
  expect(window.API.recordScore).toHaveBeenCalledTimes(1);
  const [, , patch] = window.API.recordScore.mock.calls[0];
  return patch.subResults || [];
}

describe('a fixed-order bout row records no team name', () => {
  it('writes empty side names for every numbered bout, with no lineup set', async () => {
    const subs = await scoreAndCapture(makeMatch());
    const numbered = subs.filter((s) => s.position > 0);
    expect(numbered.length).toBeGreaterThan(0);
    for (const s of numbered) {
      expect(s.sideA).toBe('');
      expect(s.sideB).toBe('');
    }
    // The row is not anonymous to the standings: a bout that was SCORED still
    // names the winning team, which is what attribution reads. Asserted on the
    // scored row only, and as an exact value: the old allow-list included ''
    // and folded undefined into it, so the only failing value was a third team
    // name, which two teams cannot produce. A regression that stopped writing
    // `winner` passed it.
    const scored = numbered.filter(s => (s.ipponsA || []).length || (s.ipponsB || []).length);
    expect(scored.length).toBeGreaterThan(0);
    for (const s of scored) {
      expect(['Kyoto', 'Osaka']).toContain(s.winner);
    }
  });

  it('never writes the team name into a numbered row', async () => {
    const subs = await scoreAndCapture(makeMatch());
    const numbered = subs.filter((x) => x.position > 0);
    // Guarded like its sibling: without this the body never runs if buildPatch
    // stops emitting numbered rows, and the test passes by iterating nothing.
    expect(numbered.length).toBeGreaterThan(0);
    for (const s of numbered) {
      expect(s.sideA).not.toBe('Kyoto');
      expect(s.sideB).not.toBe('Osaka');
    }
  });

  it('keeps the team names on the daihyosen row, where the hantei mark needs them', async () => {
    // Same shape as team_daihyosen_silence.render.test.jsx's fixture, which
    // is the one known-good way to get a daihyosen row through this harness.
    const match = makeMatch({
      subResults: [{ position: -1, sideA: 'Kyoto', sideB: 'Osaka', decision: 'daihyosen' }],
    });
    const subs = await scoreAndCapture(match);
    const dh = subs.find((s) => s.position === -1);
    expect(dh).toBeTruthy();
    expect(dh.sideA).toBe('Kyoto');
    expect(dh.sideB).toBe('Osaka');
  });
});
