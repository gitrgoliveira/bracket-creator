// mp-yqxn.1: individual (kendo) ScoreEditorModal config matrix — the same
// matrix applied to the team editor, minus the axes that don't exist for
// individuals (teamSize, teamMatchType):
//   {format(+phase)} × {naginata: on, off} × {maxEnchoPeriods: 0, 2}
//
// Config reaches the individual editor ONLY via the async competition fetch
// (window.API.fetchCompetitionDetails → naginata + maxEnchoPeriods); the
// format/phase axes ride on the match stamps.
//
// REGRESSION-PIN ONLY (epic mp-yqxn constraint): DOM assertions prove nothing
// about rendering, friction, or legibility under time pressure. Judgement
// calls belong to the browser-review children named in the comments.

import React from 'react';
import { render, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { FORMAT_PHASES, IMPOSSIBLE_FORMAT_PHASES, cellKey, NAGINATA, MAX_ENCHO, KENDO_SET, NAGINATA_SET } from './score_editor_matrix_axes.js';

const STUBBED_GLOBALS = {
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
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

// Shared axes + letter tables live in score_editor_matrix_axes.js (kept in
// lockstep with the team-editor matrix).
const CELLS = FORMAT_PHASES.flatMap(fp =>
  NAGINATA.flatMap(naginata =>
    MAX_ENCHO.map(maxEncho => ({ ...fp, naginata, maxEncho }))
  )
);

function cellName(c) {
  return `${c.format}/${c.phase} naginata=${c.naginata} maxEncho=${c.maxEncho}`;
}

function makeMatch(cell, overrides = {}) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'running',
    phase: cell.phase,
    court: 'A',
    compFormat: cell.format,
    ...(cell.phase === 'pool'
      ? { poolName: 'Pool 1' }
      : { round: 'Semi-final', matchNumber: 1 }),
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ...overrides,
  };
}

async function renderCell(cell, matchOverrides = {}, props = {}) {
  window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
    id: 'comp1',
    config: {
      format: cell.format,
      naginata: cell.naginata,
      maxEnchoPeriods: cell.maxEncho,
      players: [],
    },
  });
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal
        match={makeMatch(cell, matchOverrides)}
        onClose={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        password=""
        {...props}
      />
    );
  });
  return utils;
}

describe('individual ScoreEditorModal config matrix (running match, admin surface)', () => {
  it.each(CELLS.map(c => [cellName(c), c]))('%s', async (_name, cell) => {
    const { container } = await renderCell(cell);

    // Sune: the ippon buttons gain "S" ONLY for naginata competitions
    // (config fetch → getIpponButtons(isNaginata)). Two button strips render
    // (one per side), so compare the SET of letters.
    const letters = new Set(
      [...container.querySelectorAll('.ipt-btn')].map(b => b.textContent)
    );
    expect(letters).toEqual(cell.naginata ? NAGINATA_SET : KENDO_SET);

    // Hikiwake: the draw button is DISABLED for knockout matches (no draws in
    // elimination — decide by hantei after encho) and enabled for pool-shaped
    // matches. The individual editor derives knockout from m.phase ALONE
    // (isKnockoutPhase = phase === "bracket"): unlike the team editor there is
    // no compFormat fallback clause. Pinned; ruled on by mp-yqxn.2.
    const drawBtn = screen.getByTestId('scoring-modal-mark-draw');
    expect(drawBtn.disabled).toBe(cell.phase === 'bracket');

    // Encho affordance: collapsed pill, always present on a running match.
    expect(screen.queryByTestId('scoring-modal-encho-pill')).not.toBeNull();

    // Hantei row: surfaces whenever the scoreline is TIED (a fresh 0–0 is
    // tied), in every phase — hantei is not knockout-gated here.
    expect(screen.queryByTestId('scoring-modal-hantei-row')).not.toBeNull();

    // Admin decision controls (kiken/fusenpai).
    expect(screen.queryByTestId('scoring-modal-kiken-voluntary-button')).not.toBeNull();
    expect(screen.queryByTestId('scoring-modal-kiken-injury-button')).not.toBeNull();
    expect(screen.queryByTestId('scoring-modal-fusenpai-button')).not.toBeNull();
  });
});

describe('individual editor IMPOSSIBLE CELLS (asserted, not skipped)', () => {
  // Expectations are pinned explicitly, mirroring the team suite's map (its
  // knockout gate has a format clause this editor lacks, so a shared derived
  // expectation cannot exist). Pinned:
  //   playoffs × pool      → draw ALLOWED: a mis-stamped playoffs match could
  //                          record a hikiwake, which knockout advancement
  //                          cannot consume. Ruled on by mp-yqxn.2.
  //   league|swiss × bracket → draw blocked by the phase stamp alone. Ruled on
  //                          by mp-yqxn.3 (league) / mp-yqxn.4 (swiss).
  const IMPOSSIBLE_EXPECT_DRAW_DISABLED = {
    'playoffs/pool': false,
    'league/bracket': true,
    'swiss/bracket': true,
  };

  // Both directions at once: a newly-derived cell with no pinned expectation
  // AND a stale expectation for a cell that became product-possible each fail
  // this 1:1 check by name.
  it('expectation map stays 1:1 with the derived impossible-cell list', () => {
    expect(Object.keys(IMPOSSIBLE_EXPECT_DRAW_DISABLED).sort()).toEqual(
      IMPOSSIBLE_FORMAT_PHASES.map(cellKey).sort()
    );
  });

  it.each(IMPOSSIBLE_FORMAT_PHASES.map(fp => [`${fp.format} × phase "${fp.phase}"`, fp]))(
    '%s: product-impossible; the phase stamp alone decides the draw gate',
    async (_name, fp) => {
      await renderCell({ ...fp, naginata: false, maxEncho: 0 });
      expect(screen.getByTestId('scoring-modal-mark-draw').disabled).toBe(IMPOSSIBLE_EXPECT_DRAW_DISABLED[cellKey(fp)]);
    }
  );
});

describe('individual editor selfReport (public self-run surface)', () => {
  it('selfReport hides the kiken/fusenpai controls but KEEPS the hantei row on a tied scoreline', async () => {
    // Pinned CURRENT behaviour: the hantei affordance has its own
    // tied-scoreline condition and deliberately still surfaces in self-report
    // mode, while the admin-only withdrawal controls hide. Whether the public
    // self-run surface should be able to record a judges' decision is ruled
    // on by mp-yqxn.5.
    await renderCell(
      { format: 'mixed', phase: 'pool', naginata: false, maxEncho: 0 },
      {},
      { selfReport: true }
    );
    expect(screen.queryByTestId('scoring-modal-kiken-voluntary-button')).toBeNull();
    expect(screen.queryByTestId('scoring-modal-kiken-injury-button')).toBeNull();
    expect(screen.queryByTestId('scoring-modal-fusenpai-button')).toBeNull();
    expect(screen.queryByTestId('scoring-modal-hantei-row')).not.toBeNull();
  });
});
