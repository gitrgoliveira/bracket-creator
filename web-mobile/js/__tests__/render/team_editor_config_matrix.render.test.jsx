// mp-yqxn.1: TeamScoreEditorModal config matrix.
//
// Mounts the REAL team editor (via the ScoreEditorModal dispatch, exactly as
// every mount site does) across the full config matrix
//   {format(+phase)} × {teamSize: 3, 5} × {teamMatchType: fixed, kachinuki}
//   × {naginata: on, off}
// and asserts, per cell, WHICH controls render. Config reaches the editor two
// ways, mirroring production: match-level stamps (compFormat, teamMatchType —
// stamped by viewer_utils.compMatches) and the async competition fetch
// (naginata — via window.API.fetchCompetitionDetails).
//
// Formats map to phases: playoffs has ONLY bracket matches; league and swiss
// have ONLY pool-shaped matches; mixed has both. Cells outside that mapping
// are product-impossible and asserted as such in the IMPOSSIBLE CELLS block
// below, not silently skipped.
//
// REGRESSION-PIN ONLY (epic mp-yqxn constraint): DOM assertions prove nothing
// about rendering, friction, or legibility under time pressure. Judgement
// calls belong to the browser-review children named in the comments.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { FORMAT_PHASES, IMPOSSIBLE_FORMAT_PHASES, cellKey, NAGINATA, KENDO_SET, NAGINATA_SET } from './score_editor_matrix_axes.js';

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

// ── matrix ───────────────────────────────────────────────────────────────────

// Shared axes + letter tables live in score_editor_matrix_axes.js (kept in
// lockstep with the individual-editor matrix). Team-only axes stay here.
const TEAM_SIZES = [3, 5];
const MATCH_TYPES = ['fixed', 'kachinuki'];

const CELLS = FORMAT_PHASES.flatMap(fp =>
  TEAM_SIZES.flatMap(teamSize =>
    MATCH_TYPES.flatMap(tmt =>
      NAGINATA.map(naginata => ({ ...fp, teamSize, tmt, naginata }))
    )
  )
);

function cellName(c) {
  return `${c.format}/${c.phase} size=${c.teamSize} ${c.tmt} naginata=${c.naginata}`;
}

function makeTeamMatch(cell, overrides = {}) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'running',
    phase: cell.phase,
    court: 'A',
    compKind: 'team',
    teamSize: cell.teamSize,
    compFormat: cell.format,
    teamMatchType: cell.tmt,
    ...(cell.phase === 'pool'
      ? { poolName: 'Pool 1' }
      : { round: 'Semi-final', matchNumber: 1 }),
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    ...overrides,
  };
}

// Mock the competition fetch for this cell, mount through the dispatcher, and
// flush the async compMeta effect inside act() so assertions see settled state.
async function renderCell(cell, matchOverrides = {}, props = {}) {
  window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
    id: 'comp1',
    config: {
      format: cell.format,
      teamMatchType: cell.tmt,
      naginata: cell.naginata,
      players: [],
    },
  });
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal
        match={makeTeamMatch(cell, matchOverrides)}
        onClose={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        password=""
        {...props}
      />
    );
  });
  return utils;
}

describe('TeamScoreEditorModal config matrix (running match, admin surface)', () => {
  it.each(CELLS.map(c => [cellName(c), c]))('%s', async (_name, cell) => {
    const { container } = await renderCell(cell);

    // Bout rows: fixed renders every position; kachinuki renders ONLY the
    // current bout (bootstrap: bout 1 on a fresh match) — see
    // kachinukiVisiblePositions. Whether hiding earlier/later bouts is the
    // right operator affordance is ruled on by mp-yqxn.2.
    const rows = container.querySelectorAll('.team-sub-match');
    expect(rows.length).toBe(cell.tmt === 'kachinuki' ? 1 : cell.teamSize);

    // Sune: the per-bout ippon buttons gain "S" ONLY when the competition is
    // naginata (admin_scoring_team.jsx getIpponButtons(isNaginataTeam)).
    const letters = new Set(
      [...container.querySelectorAll('.ipt-btn')].map(b => b.textContent)
    );
    expect(letters).toEqual(cell.naginata ? NAGINATA_SET : KENDO_SET);

    // Encho affordance: the top overtime pill is present for FIXED formats
    // (collapsed while no overtime is active; the stepper itself is now
    // unbounded above per mp-m4bn). mp-gmcg: in a running kachinuki match
    // (bout mode) the top pill is suppressed — declaring encho there is the
    // optional footer Encho on a tied bout, not a top period-stepper — so the
    // pill must be ABSENT for kachinuki cells.
    if (cell.tmt === 'kachinuki') {
      expect(screen.queryByTestId('scoring-modal-encho-pill')).toBeNull();
    } else {
      expect(screen.queryByTestId('scoring-modal-encho-pill')).not.toBeNull();
    }

    // Daihyosen affordance is knockout-only (T141): phase "bracket", or the
    // playoffs/mixed fallback for non-pool phases. Pool matches resolve ties
    // via standings + the auto-injected pool daihyosen instead. mp-gmcg:
    // daihyosen does not exist in kachinuki (a tied final bout goes to encho
    // on that same bout), so the ADD affordance is hidden for kachinuki even
    // in a knockout — the tie resolves via the inline Encho path instead.
    const expectDaihyosen = cell.phase === 'bracket' && cell.tmt !== 'kachinuki';
    expect(!!screen.queryByTestId('scoring-modal-daihyosen-button')).toBe(expectDaihyosen);

    // Admin decision controls (kiken/fusenpai) render on the admin surface.
    expect(screen.queryByTestId('scoring-modal-kiken-voluntary-button')).not.toBeNull();
    expect(screen.queryByTestId('scoring-modal-kiken-injury-button')).not.toBeNull();
    expect(screen.queryByTestId('scoring-modal-fusenpai-button')).not.toBeNull();

    // Per-bout tie + fusensho affordances scale with the visible rows.
    expect(screen.queryAllByTestId('scoring-modal-tie-button').length).toBe(rows.length);
    expect(screen.queryAllByTestId('scoring-modal-fusensho-button').length).toBe(rows.length * 2);
  });
});

describe('TeamScoreEditorModal IMPOSSIBLE CELLS (asserted, not skipped)', () => {
  // kachinuki × teamSize<2 is rejected at create/settings time
  // (state.ValidateTeamMatchType: "kachinuki requires teamSize >= 2"), so the
  // matrix's teamSize axis {3,5} already covers every legal kachinuki size
  // bracket. Nothing to mount for size<2; recorded here so the cell is
  // visibly excluded rather than forgotten.

  // Expectations are pinned explicitly because the team editor's knockout gate
  // is TWO clauses (phase === "bracket" OR playoffs/mixed with a non-pool
  // phase, admin_scoring_team.jsx isKnockoutPhase) — deriving the expected
  // value from phase alone would silently mis-pin a future cell exercising the
  // format clause. Pinned:
  //   playoffs × pool      → NON-knockout: no in-match daihyosen (a drawn pool
  //                          match must never grow one). Ruled on by mp-yqxn.2.
  //   league|swiss × bracket → the phase clause runs FIRST, so the daihyosen
  //                          affordance renders although the round-robin format
  //                          has no rules for it. Ruled on by mp-yqxn.3
  //                          (league) / mp-yqxn.4 (swiss).
  const IMPOSSIBLE_EXPECT_DAIHYOSEN = {
    'playoffs/pool': false,
    'league/bracket': true,
    'swiss/bracket': true,
  };

  // Both directions at once: a newly-derived cell with no pinned expectation
  // AND a stale expectation for a cell that became product-possible each fail
  // this 1:1 check by name.
  it('expectation map stays 1:1 with the derived impossible-cell list', () => {
    expect(Object.keys(IMPOSSIBLE_EXPECT_DAIHYOSEN).sort()).toEqual(
      IMPOSSIBLE_FORMAT_PHASES.map(cellKey).sort()
    );
  });

  it.each(IMPOSSIBLE_FORMAT_PHASES.map(fp => [`${fp.format} × phase "${fp.phase}"`, fp]))(
    '%s: product-impossible; the editor trusts the phase stamp',
    async (_name, fp) => {
      await renderCell({ ...fp, teamSize: 5, tmt: 'fixed', naginata: false });
      expect(!!screen.queryByTestId('scoring-modal-daihyosen-button')).toBe(IMPOSSIBLE_EXPECT_DAIHYOSEN[cellKey(fp)]);
    }
  );
});

describe('TeamScoreEditorModal selfReport (public self-run surface)', () => {
  it('selfReport hides the admin decision controls', async () => {
    await renderCell(
      { format: 'mixed', phase: 'pool', teamSize: 5, tmt: 'fixed', naginata: false },
      {},
      { selfReport: true }
    );
    expect(screen.queryByTestId('scoring-modal-kiken-voluntary-button')).toBeNull();
    expect(screen.queryByTestId('scoring-modal-fusenpai-button')).toBeNull();
    // Scoring itself stays available.
    expect(screen.queryAllByTestId('scoring-modal-tie-button').length).toBeGreaterThan(0);
  });
});

describe('TeamScoreEditorModal encho stepper is unbounded (mp-m4bn)', () => {
  async function expandEncho() {
    await act(async () => {
      fireEvent.click(screen.getByTestId('scoring-modal-encho-pill'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('scoring-modal-encho-checkbox'));
    });
  }

  it('the + stepper never disables, however many periods were fought', async () => {
    // mp-m4bn: unbounded by design — see the nextEnchoPeriod docstring in
    // admin_scoring_shared.jsx. This is the end-to-end pin: the button never
    // disables and there is no "maximum reached" alert to show.
    await renderCell({ format: 'mixed', phase: 'bracket', teamSize: 5, tmt: 'fixed', naginata: false });
    await expandEncho();
    const inc = screen.getByRole('button', { name: 'Increase overtime period count' });
    for (let i = 0; i < 6; i++) {
      expect(inc.disabled).toBe(false);
      await act(async () => { fireEvent.click(inc); });
    }
    expect(inc.disabled).toBe(false);
    expect(screen.queryByRole('alert')).toBeNull();
  });
});

describe('TeamScoreEditorModal kachinuki bout navigation', () => {
  const KACHI_CELL = { format: 'playoffs', phase: 'bracket', teamSize: 5, tmt: 'kachinuki', naginata: false };

  it('RUNNING: the fought bouts render as read-only rows above the current editable bout', async () => {
    // Server bout log: bout 1 fought (won by A1), bout 2 appended by
    // engine.AdvanceKachinuki and not yet scored. Both render (mp-gmcg): bout 1
    // as a READ-ONLY team-sheet row (no scoring controls) above bout 2, the
    // current editable bout — the operator sees the whole encounter like a
    // regular team sheet. The read-only row is TAPPABLE to reopen it inline
    // for correction (mp-gmcg; see the correction tests below); until tapped
    // it carries no scoring controls.
    const { container } = await renderCell(KACHI_CELL, {
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M', 'K'], ipponsB: [], winner: 'A1' },
        { position: 2, sideA: 'A1', sideB: 'B2', ipponsA: [], ipponsB: [] },
      ],
    });
    const rows = container.querySelectorAll('.team-sub-match');
    expect(rows.length).toBe(2);
    expect([...rows].map(r => r.querySelector('.team-sub-match__pos-num').textContent)).toEqual(['1', '2']);
    // Bout 1 (fought) is read-only — no scoring controls; bout 2 (current) is editable.
    expect(rows[0].classList.contains('team-sub-match--readonly')).toBe(true);
    expect(rows[0].querySelector('.team-sub-match__btns')).toBeNull();
    expect(rows[1].classList.contains('team-sub-match--readonly')).toBe(false);
    expect(rows[1].querySelector('.team-sub-match__btns')).not.toBeNull();
  });

  it('RUNNING: a fought hikiwake bout shows an X on its read-only row', async () => {
    // Bout 1 fought to a hikiwake (both retire), bout 2 appended and unscored.
    // The read-only row for bout 1 carries the same centre X mark as an editable
    // tie, so a draw in the fought history is unambiguous.
    const { container } = await renderCell(KACHI_CELL, {
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: [], decision: 'hikiwake' },
        { position: 2, sideA: 'A2', sideB: 'B2', ipponsA: [], ipponsB: [] },
      ],
    });
    const rows = container.querySelectorAll('.team-sub-match');
    expect(rows.length).toBe(2);
    expect(rows[0].classList.contains('team-sub-match--readonly')).toBe(true);
    expect(rows[0].querySelector('.tsm-draw')?.textContent).toBe('X');
    // No winner is bolded on a tie.
    expect(rows[0].querySelector('.tsm-name__static--win')).toBeNull();
  });

  it('RUNNING: the keyboard cannot score a bout that is already decided (no impossible 2-2)', async () => {
    // Regression for the /simplify catch: keyboard scoring must honour the same
    // guards as the ippon buttons (disabled once decided, capped per side), so
    // it can't record a score the taps forbid.
    const { container } = await renderCell(KACHI_CELL, {
      subResults: [{ position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: [] }],
    });
    const filled = (side) => container.querySelectorAll(`.tsm-center-pts--${side} .editor-side__pt--filled`).length;
    // Score Shiro (no shift) to 2 ippons via the keyboard: the bout is now decided.
    await act(async () => { fireEvent.keyDown(document.body, { key: 'm' }); });
    await act(async () => { fireEvent.keyDown(document.body, { key: 'k' }); });
    expect(filled('shiro')).toBe(2);
    // A further Aka keystroke (Shift+M) is a no-op — a decided bout can't reach 2-2.
    await act(async () => { fireEvent.keyDown(document.body, { key: 'M', shiftKey: true }); });
    expect(filled('aka')).toBe(0);
  });

  it('RUNNING: tapping a fought bout reopens it inline with scoring controls (mp-gmcg)', async () => {
    // The mid-encounter correction path: a fought bout is read-only with a
    // visible "Correct" affordance, and tapping it reopens the SAME scoring
    // controls as the current bout, in place, flagged as the bout being
    // corrected. No Reopen/End round-trip needed.
    const { container } = await renderCell(KACHI_CELL, {
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M', 'K'], winner: 'B1' },
        { position: 2, sideA: 'A2', sideB: 'B1', ipponsA: [], ipponsB: [] },
      ],
    });
    const done = screen.getByTestId('kachinuki-done-bout-0');
    expect(done.classList.contains('team-sub-match--readonly')).toBe(true);
    expect(done.querySelector('.team-sub-match__edit-hint')).not.toBeNull();
    expect(done.querySelector('.team-sub-match__btns')).toBeNull();
    await act(async () => { fireEvent.click(done); });
    const rows = container.querySelectorAll('.team-sub-match');
    expect(rows.length).toBe(2);
    // Bout 1 now editable + flagged as being corrected; bout 2 stays editable.
    expect(rows[0].classList.contains('team-sub-match--correcting')).toBe(true);
    expect(rows[0].querySelector('.team-sub-match__btns')).not.toBeNull();
    expect(rows[1].querySelector('.team-sub-match__btns')).not.toBeNull();
    expect(screen.getByTestId('kachinuki-done-edit-footer-0')).toBeTruthy();
  });

  it('RUNNING: correcting a fought bout without changing the winner shows no chain warning (mp-gmcg)', async () => {
    // Shiro (sideB) won 2-1. Removing the loser's (Aka) ippon is a pure score
    // fix: the winner is unchanged, so the later bouts are unaffected and no
    // warning shows.
    const { container } = await renderCell(KACHI_CELL, {
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: ['M', 'K'], winner: 'B1' },
        { position: 2, sideA: 'A2', sideB: 'B1', ipponsA: [], ipponsB: [] },
      ],
    });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-done-bout-0')); });
    const bout0 = container.querySelectorAll('.team-sub-match')[0];
    const akaFilled = bout0.querySelectorAll('.tsm-center-pts--aka .editor-side__pt--filled');
    expect(akaFilled.length).toBe(1);
    await act(async () => { fireEvent.click(akaFilled[0]); });
    expect(screen.queryByTestId('kachinuki-done-edit-warn')).toBeNull();
  });

  it('RUNNING: changing who won a fought bout warns the later bouts need re-checking (mp-gmcg)', async () => {
    // Shiro (sideB) won 2-1. Removing a winner ippon makes it 1-1 — no longer
    // Shiro's win — which reshapes who-stays-on for every later bout. The app
    // never restacks them itself (only the courtside operator knows the real
    // later results), so it warns instead.
    const { container } = await renderCell(KACHI_CELL, {
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: ['M', 'K'], winner: 'B1' },
        { position: 2, sideA: 'A2', sideB: 'B1', ipponsA: [], ipponsB: [] },
      ],
    });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-done-bout-0')); });
    expect(screen.queryByTestId('kachinuki-done-edit-warn')).toBeNull();
    const bout0 = container.querySelectorAll('.team-sub-match')[0];
    const shiroFilled = bout0.querySelectorAll('.tsm-center-pts--shiro .editor-side__pt--filled');
    expect(shiroFilled.length).toBe(2);
    await act(async () => { fireEvent.click(shiroFilled[0]); });
    expect(screen.getByTestId('kachinuki-done-edit-warn')).toBeTruthy();
  });

  it('RUNNING: Done collapses a corrected bout back to a read-only row (mp-gmcg)', async () => {
    await renderCell(KACHI_CELL, {
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M', 'K'], winner: 'B1' },
        { position: 2, sideA: 'A2', sideB: 'B1', ipponsA: [], ipponsB: [] },
      ],
    });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-done-bout-0')); });
    expect(screen.getByTestId('kachinuki-done-edit-footer-0')).toBeTruthy();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-done-edit-done')); });
    expect(screen.getByTestId('kachinuki-done-bout-0').classList.contains('team-sub-match--readonly')).toBe(true);
    expect(screen.queryByTestId('kachinuki-done-edit-footer-0')).toBeNull();
  });

  it('RUNNING: an open past-bout correction survives a same-match reload (mp-gmcg)', async () => {
    // Autosave persists each correction as a running write that round-trips back
    // over SSE as a fresh `match` object with the SAME id. The editor must stay
    // open across that reload (keyed on match id, not object identity) instead of
    // collapsing after every ippon change.
    const overrides = {
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M', 'K'], winner: 'B1' },
        { position: 2, sideA: 'A2', sideB: 'B1', ipponsA: [], ipponsB: [] },
      ],
    };
    const utils = await renderCell(KACHI_CELL, overrides);
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-done-bout-0')); });
    expect(screen.getByTestId('kachinuki-done-edit-footer-0')).toBeTruthy();
    // Simulate the SSE reload: a brand-new match object, same id + data.
    await act(async () => {
      utils.rerender(
        <ScoreEditorModal match={makeTeamMatch(KACHI_CELL, overrides)} onClose={vi.fn()}
          onSubmit={vi.fn().mockResolvedValue(undefined)} password="" />
      );
    });
    expect(screen.getByTestId('kachinuki-done-edit-footer-0')).toBeTruthy();
  });

  it('COMPLETED (correction): every fought server bout renders and is editable', async () => {
    const { container } = await renderCell(KACHI_CELL, {
      status: 'completed',
      winner: { id: 'team-A', name: 'Team A' },
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M', 'K'], ipponsB: [], winner: 'A1' },
        { position: 2, sideA: 'A1', sideB: 'B2', ipponsA: ['D'], ipponsB: ['M'], winner: '' },
      ],
    });
    const nums = [...container.querySelectorAll('.team-sub-match__pos-num')].map(n => n.textContent);
    expect(nums).toEqual(['1', '2']);
  });
});
