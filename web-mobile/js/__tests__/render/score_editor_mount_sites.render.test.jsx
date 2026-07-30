// mp-yqxn.1: cross-surface invariants — the five ScoreEditorModal mount sites.
//
// Five surfaces mount the score editor and they DISAGREE on affordances:
//   1. admin_shiaijo.jsx        inline; Finish + Start Next; cannot close
//   2. admin_pools.jsx          modal; no chaining (onSubmitAndNext null)
//   3. admin_competition_bracket.jsx  inline; no chaining; close only when complete
//   4. admin_schedule_score_editor.jsx  modal; Prev/Next + Finish-and-next
//   5. viewer_match.jsx         public self-run; bare submit only
//
// Whether that divergence is a DEFECT is for the mp-yqxn review children to
// decide (mp-yqxn.2–.5). This file only makes the divergence VISIBLE and
// regression-proof: each surface's wiring is pinned as an exact prop-kind
// table, so any future change to what a surface wires fails the build here
// and forces a deliberate update.
//
// Mechanism: window.ScoreEditorModal is replaced by a prop-recording probe
// BEFORE the surface modules load (three of the five capture it at module-eval
// time), each surface is driven until its editor mounts, and the recorded prop
// bag is normalized to {absent|null|fn|value} kinds and compared exactly.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';

// ── the probe ────────────────────────────────────────────────────────────────

const probe = { props: null };
function ProbeScoreEditor(props) {
  probe.props = props;
  return React.createElement('div', { 'data-testid': 'probe-score-editor' });
}

// Normalize a prop to a stable kind so tables diff cleanly.
const kind = (v) =>
  v === undefined ? 'absent'
  : v === null ? 'null'
  : typeof v === 'function' ? 'fn'
  : typeof v === 'object' ? 'object'
  : v;

// The invariant surface under test: every wiring-relevant prop of the editor.
function wiringOf(p) {
  return {
    onSubmit: kind(p.onSubmit),
    onSubmitAndNext: kind(p.onSubmitAndNext),
    onAfterDecision: kind(p.onAfterDecision),
    onPrev: kind(p.onPrev),
    onNext: kind(p.onNext),
    prevMatch: kind(p.prevMatch),
    nextMatch: kind(p.nextMatch),
    onClose: kind(p.onClose),
    canClose: kind(p.canClose),
    variant: kind(p.variant),
    password: kind(p.password) === 'absent' ? 'absent' : p.password,
    selfReport: kind(p.selfReport),
  };
}

// ── window stubs (superset of the per-surface render tests) ──────────────────
// hasBothSides / hasPoolOriginPlaceholder / pluralize / Icon / EmptyState /
// useEscapeToClose / poolLabel are the REAL implementations published by
// vitest.setup.render.js (admin_helpers.jsx, ui.jsx, viewer_utils.jsx).

const STUBBED_GLOBALS = {
  // MODULE-EVAL captures: must exist before the imports in beforeAll
  ScoreEditorModal: ProbeScoreEditor,
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  BracketTree: () => null, // per-test override drives bracket selection
  getScoreBtnClass: () => 'test-score-open',
  ipponsFromScore: () => [],
  matchScoreStr: () => '',
  // LAZY / render-time
  filterMatchesByCourt: (matches) => matches,
  filterMatchesByPhase: (matches) => matches,
  tournamentMatches: () => [],
  compMatches: () => [],
  startPatch: () => ({ status: 'running', winner: null }),
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  PoolsViewer: () => null, // per-test override drives pools open
  LeagueStandingsViewer: () => null,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    fetchCourtMatches: vi.fn().mockResolvedValue([]),
    subscribeToEvents: () => () => {},
    recordScore: vi.fn().mockResolvedValue(undefined),
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
  },
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const originals = {};
let AdminShiaijoPage, AdminPools, AdminBracket, AdminScoreEditor, MatchViewerModal;

beforeAll(async () => {
  // jsdom doesn't implement scrollTo; the schedule surface calls it on open.
  window.scrollTo = vi.fn();
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_shiaijo.jsx');
  await import('../../admin_pools.jsx');
  await import('../../admin_competition_bracket.jsx');
  const sched = await import('../../admin_schedule_score_editor.jsx');
  const viewer = await import('../../viewer_match.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
  AdminPools = window.AdminPools;
  AdminBracket = window.AdminBracket;
  AdminScoreEditor = sched.AdminScoreEditor;
  MatchViewerModal = viewer.MatchViewerModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

beforeEach(() => {
  probe.props = null;
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
  window.PoolsViewer = STUBBED_GLOBALS.PoolsViewer;
  window.BracketTree = STUBBED_GLOBALS.BracketTree;
  window.compMatches = STUBBED_GLOBALS.compMatches;
});

function runningMatch(overrides = {}) {
  return {
    id: 'm1', compId: 'c1', compName: 'Comp', status: 'running',
    phase: 'pool', poolName: 'Pool 1', court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ...overrides,
  };
}

// ── 1. admin_shiaijo.jsx: inline court console ───────────────────────────────

describe('mount site: admin_shiaijo.jsx (court console)', () => {
  it('wires Finish+StartNext and after-decision advance; inline, cannot close, NO Prev/Next', async () => {
    window.tournamentMatches = () => [runningMatch()];
    await act(async () => {
      render(
        <AdminShiaijoPage
          tournament={{ name: 'T', courts: ['A'], competitions: [] }}
          court="A"
          onBack={vi.fn()} onEditScore={vi.fn()} onMoveCourt={vi.fn()}
          onLogout={vi.fn()} onViewerMode={vi.fn()} password="pw"
          showToast={vi.fn()} tweaks={{}} onSwitchCourt={vi.fn()}
        />
      );
    });
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
    expect(wiringOf(probe.props)).toEqual({
      onSubmit: 'fn',
      onSubmitAndNext: 'fn',       // Finish + Start Next (court flow)
      onAfterDecision: 'fn',       // fusenpai/kiken also advances the court
      onPrev: 'absent',            // court console has no match navigation
      onNext: 'absent',
      prevMatch: 'absent',
      nextMatch: 'absent',
      onClose: 'fn',               // wired but a no-op: () => {}
      canClose: false,             // the inline editor IS the page
      variant: 'inline',
      password: 'pw',
      selfReport: 'absent',
    });
  });
});

// ── 2. admin_pools.jsx: pool card modal ──────────────────────────────────────

describe('mount site: admin_pools.jsx (pools tab)', () => {
  it('wires a bare modal: NO chaining (onSubmitAndNext null) and NO Prev/Next', async () => {
    const rawPoolMatch = {
      id: 'Pool 1-1', status: 'scheduled',
      sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
    };
    // PoolsViewer probe: expose the surface's onMatchClick as a button.
    window.PoolsViewer = (props) => (
      <button data-testid="open-pool-match" onClick={() => props.onMatchClick(rawPoolMatch)}>open</button>
    );
    await act(async () => {
      render(
        <AdminPools
          c={{ id: 'c1', name: 'Comp', format: 'mixed', kind: 'individual', status: 'started' }}
          pools={[{ name: 'Pool 1', players: [] }]}
          poolMatches={[]}
          standings={[]}
          tweaks={{}}
          onEditScore={vi.fn()}
          password="pw"
        />
      );
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('open-pool-match'));
    });
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
    expect(wiringOf(probe.props)).toEqual({
      onSubmit: 'fn',
      onSubmitAndNext: 'null',     // explicitly null: no chaining from pools
      onAfterDecision: 'absent',
      onPrev: 'null',              // explicitly null: no navigation
      onNext: 'null',
      prevMatch: 'null',
      nextMatch: 'null',
      onClose: 'fn',
      canClose: 'absent',          // default (true): a modal can close
      variant: 'absent',           // default modal
      password: 'pw',
      selfReport: 'absent',
    });
  });
});

// ── 3. admin_competition_bracket.jsx: bracket running panel ──────────────────

describe('mount site: admin_competition_bracket.jsx (bracket panel)', () => {
  it('wires an inline no-chain editor; close only when the match is complete', async () => {
    const bm = runningMatch({ id: 'bm1', phase: undefined, poolName: undefined });
    delete bm.phase;
    delete bm.poolName;
    // BracketTree probe: expose the tree's onMatchClick as a button.
    window.BracketTree = (props) => (
      <button data-testid="open-bracket-match" onClick={() => props.onMatchClick(bm, 0, 0)}>open</button>
    );
    await act(async () => {
      render(
        <AdminBracket
          c={{ id: 'c1', name: 'Comp', engi: false }}
          t={{ courts: ['A'] }}
          bracket={{ rounds: [[bm]] }}
          onMoveCourt={vi.fn()}
          onEditScore={vi.fn()}
          tweaks={{}}
          password="pw"
        />
      );
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('open-bracket-match'));
    });
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
    expect(wiringOf(probe.props)).toEqual({
      onSubmit: 'fn',
      onSubmitAndNext: 'null',     // explicitly null: no chaining from the bracket
      onAfterDecision: 'absent',
      onPrev: 'absent',
      onNext: 'absent',
      prevMatch: 'absent',
      nextMatch: 'absent',
      onClose: 'fn',
      canClose: false,             // running match: nowhere to fall back to
      variant: 'inline',
      password: 'pw',
      selfReport: 'absent',
    });
    // The bracket panel stamps phase "bracket" on the editor's match so the
    // no-draw knockout rule holds (AdminBracket.scoringMatch enrichment).
    expect(probe.props.match.phase).toBe('bracket');
  });
});

// ── 4. admin_schedule_score_editor.jsx: Scores tab ───────────────────────────

describe('mount site: admin_schedule_score_editor.jsx (Scores tab)', () => {
  it('wires the FULL navigation set: Prev/Next, Finish+StartNext, after-decision', async () => {
    const m1 = runningMatch();
    const m2 = runningMatch({ id: 'm2', status: 'scheduled' });
    window.compMatches = () => [m1, m2];
    await act(async () => {
      render(
        <AdminScoreEditor
          t={{ competitions: [{ id: 'c1', name: 'Comp' }] }}
          onEditScore={vi.fn()}
          onMoveCourt={null}
          password="pw"
          showToast={vi.fn()}
        />
      );
    });
    // Open the first (running) match via its row button.
    const openBtns = document.querySelectorAll('button.test-score-open');
    expect(openBtns.length).toBe(2);
    await act(async () => { fireEvent.click(openBtns[0]); });
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
    expect(wiringOf(probe.props)).toEqual({
      onSubmit: 'fn',
      onSubmitAndNext: 'fn',       // next same-court active match exists (m2)
      onAfterDecision: 'fn',
      onPrev: 'fn',
      onNext: 'fn',
      prevMatch: 'null',           // m1 is the first match on this court
      nextMatch: 'object',         // m2: same-shiaijo chain (see pitfall note)
      onClose: 'fn',
      canClose: 'absent',          // default (true): modal can close
      variant: 'absent',           // default modal
      password: 'pw',
      selfReport: 'absent',
    });
    // Chained navigation must stay on the current match's shiaijo (CLAUDE.md
    // pitfall): with both fixtures on court A, m2 is the wired next match.
    expect(probe.props.nextMatch.id).toBe('m2');
  });

  it('onSubmitAndNext is NULL when no same-court active match remains', async () => {
    window.compMatches = () => [runningMatch()];
    await act(async () => {
      render(
        <AdminScoreEditor
          t={{ competitions: [{ id: 'c1', name: 'Comp' }] }}
          onEditScore={vi.fn()} onMoveCourt={null} password="pw" showToast={vi.fn()}
        />
      );
    });
    await act(async () => { fireEvent.click(document.querySelector('button.test-score-open')); });
    expect(wiringOf(probe.props).onSubmitAndNext).toBe('null');
    expect(wiringOf(probe.props).onAfterDecision).toBe('null');
  });
});

// ── 5. viewer_match.jsx: public self-run surface ─────────────────────────────

describe('mount site: viewer_match.jsx (public self-run)', () => {
  it('wires ONLY submit/close with selfReport:true and empty password — no navigation, no chaining, no decision advance', async () => {
    // Pinned CURRENT behaviour: the public surface passes neither
    // onSubmitAndNext nor Prev/Next nor onAfterDecision. Combined with the
    // dispatch gap (the engi branch drops selfReport entirely: see
    // score_editor_dispatch.render.test.jsx), the self-run affordance set is
    // ruled on by mp-yqxn.5.
    await act(async () => {
      render(
        <MatchViewerModal
          match={runningMatch()}
          onClose={vi.fn()}
          tournament={{ mode: 'self-run' }}
          compId="c1"
        />
      );
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Report result' }));
    });
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
    expect(wiringOf(probe.props)).toEqual({
      onSubmit: 'fn',
      onSubmitAndNext: 'absent',
      onAfterDecision: 'absent',
      onPrev: 'absent',
      onNext: 'absent',
      prevMatch: 'absent',
      nextMatch: 'absent',
      onClose: 'fn',
      canClose: 'absent',          // default (true)
      variant: 'absent',           // default modal
      password: '',                // public surface authenticates nothing
      selfReport: true,
    });
  });
});
