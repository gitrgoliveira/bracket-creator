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
import { installWindowStubs } from '../helpers/stub_globals.js';

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
    password: kind(p.password),
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
  matchScoreStr: () => '',
  // LAZY / render-time
  filterMatchesByCourt: (matches) => matches,
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

let restoreGlobals;
let AdminShiaijoPage, AdminPools, AdminBracket, AdminScoreEditor, MatchViewerModal;

beforeAll(async () => {
  // jsdom doesn't implement scrollTo; the schedule surface calls it on open.
  window.scrollTo = vi.fn();
  // Nor scrollIntoView; the bracket page scrolls its panel into view on a pick.
  Element.prototype.scrollIntoView = vi.fn();
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
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

afterAll(() => restoreGlobals());

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

// ── the five surfaces, each driven until its editor mounts ───────────────────
// onEditScore is the write every admin host routes through; the public
// self-run surface calls window.API.recordScore itself instead.

async function mountShiaijo(onEditScore = vi.fn()) {
  window.tournamentMatches = () => [runningMatch()];
  await act(async () => {
    render(
      <AdminShiaijoPage
        tournament={{ name: 'T', courts: ['A'], competitions: [] }}
        court="A"
        onBack={vi.fn()} onEditScore={onEditScore} onMoveCourt={vi.fn()}
        onLogout={vi.fn()} onViewerMode={vi.fn()} password="pw"
        showToast={vi.fn()} tweaks={{}} onSwitchCourt={vi.fn()}
      />
    );
  });
}

async function mountPools(onEditScore = vi.fn()) {
  const rawPoolMatch = {
    id: 'Pool 1-1', status: 'scheduled',
    sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  };
  // PoolsViewer probe: expose the surface's onMatchClick as a button.
  window.PoolsViewer = (props) => (
    <button data-testid="open-pool-match" onClick={() => props.onMatchClick(rawPoolMatch)}>open</button>
  );
  // rawPoolMatch must be IN poolMatches below: the real PoolsViewer renders
  // its rows from that list, and AdminPools now re-resolves the open match
  // out of it every render rather than keeping the clicked object, so a
  // fixture clicking a match the list does not contain opens nothing.
  await act(async () => {
    render(
      <AdminPools
        c={{ id: 'c1', name: 'Comp', format: 'mixed', kind: 'individual', status: 'started' }}
        pools={[{ name: 'Pool 1', players: [] }]}
        poolMatches={[rawPoolMatch]}
        standings={[]}
        tweaks={{}}
        onEditScore={onEditScore}
        password="pw"
      />
    );
  });
  await act(async () => {
    fireEvent.click(screen.getByTestId('open-pool-match'));
  });
}

async function mountBracket(onEditScore = vi.fn()) {
  // Raw bracket.rounds entries carry no phase/pool stamps: strip them so the
  // panel's own enrichment (phase: "bracket") is what reaches the editor.
  const bm = runningMatch({ id: 'bm1' });
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
        onEditScore={onEditScore}
        tweaks={{}}
        password="pw"
      />
    );
  });
  await act(async () => {
    fireEvent.click(screen.getByTestId('open-bracket-match'));
  });
}

async function mountSchedule(onEditScore = vi.fn(), matches = [runningMatch(), runningMatch({ id: 'm2', status: 'scheduled' })]) {
  window.compMatches = () => matches;
  await act(async () => {
    render(
      <AdminScoreEditor
        t={{ competitions: [{ id: 'c1', name: 'Comp' }] }}
        onEditScore={onEditScore}
        onMoveCourt={null}
        password="pw"
        showToast={vi.fn()}
      />
    );
  });
}

async function mountSelfRun() {
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
}

// ── 1. admin_shiaijo.jsx: inline court console ───────────────────────────────

describe('mount site: admin_shiaijo.jsx (court console)', () => {
  it('wires Finish+StartNext and after-decision advance; inline, cannot close, NO Prev/Next', async () => {
    await mountShiaijo();
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
    await mountPools();
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
    await mountBracket();
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
    await mountSchedule();
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
    await mountSchedule(vi.fn(), [runningMatch()]);
    await act(async () => { fireEvent.click(document.querySelector('button.test-score-open')); });
    const w = wiringOf(probe.props);
    expect(w.onSubmitAndNext).toBe('null');
    expect(w.onAfterDecision).toBe('null');
  });

  // Recording a withdrawal starts the court's next match, as a no-show does
  // (mp-nwds item 7). The list shows the withdrawn competitor's own matches
  // barred only after it refreshes, so the advance passes over them itself.
  describe('the advance after a withdrawal', () => {
    const yamada = { id: 'p1', name: 'Yamada' };
    const tanaka = { id: 'p2', name: 'Tanaka' };
    const suzuki = { id: 'p3', name: 'Suzuki' };
    // What /decision answers with: the stored match, sides as bare names.
    const kikenByYamada = {
      id: 'm1', sideA: 'Yamada', sideB: 'Tanaka', sideAId: 'p1', sideBId: 'p2', winner: 'Tanaka', winnerId: 'p2',
      status: 'completed', decision: 'kiken-voluntary', decisionBy: 'aka',
    };
    const yamadaNext = runningMatch({ id: 'm2', status: 'scheduled', sideA: suzuki, sideB: yamada });
    const openFirst = async (onEditScore, matches) => {
      await mountSchedule(onEditScore, matches);
      await act(async () => { fireEvent.click(document.querySelector('button.test-score-open')); });
    };

    it('starts the next match the withdrawn competitor is not in', async () => {
      const onEditScore = vi.fn().mockResolvedValue({ status: 'running' });
      await openFirst(onEditScore, [runningMatch(), yamadaNext, runningMatch({ id: 'm3', status: 'scheduled', sideA: suzuki, sideB: tanaka })]);
      await act(async () => { await probe.props.onAfterDecision(kikenByYamada); });
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(onEditScore.mock.calls[0][1], "Yamada's next match is passed over").toBe('m3');
    });

    it('closes the editor when only the withdrawn competitor\'s matches are left', async () => {
      const onEditScore = vi.fn();
      await openFirst(onEditScore, [runningMatch(), yamadaNext]);
      await act(async () => { await probe.props.onAfterDecision(kikenByYamada); });
      expect(onEditScore).not.toHaveBeenCalled();
      expect(screen.queryByTestId('probe-score-editor')).toBeNull();
    });
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
    await mountSelfRun();
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

// ── every surface: what a write came back with ───────────────────────────────
// bc-strt: the editor's own Start match treats the match as running only for
// a write that reached the server. A refused one (court busy, a withdrawn
// competitor) throws inside the host, which reports it and hands back
// nothing. So every host must hand back what a landed write came back with,
// or no start made from its editor could ever count as landed.

describe('every mount site hands the editor what a write came back with (bc-strt)', () => {
  const openSchedule = async (onEditScore) => {
    await mountSchedule(onEditScore);
    await act(async () => { fireEvent.click(document.querySelector('button.test-score-open')); });
  };
  const sites = [
    ['admin_shiaijo.jsx', mountShiaijo],
    ['admin_pools.jsx', mountPools],
    ['admin_competition_bracket.jsx', mountBracket],
    ['admin_schedule_score_editor.jsx', openSchedule],
    ['viewer_match.jsx', async (write) => {
      window.API.recordScore = write;
      await mountSelfRun();
    }],
  ];

  it.each(sites)('%s', async (_name, mount) => {
    const landed = { status: 'running' };
    const write = vi.fn().mockResolvedValueOnce(landed).mockRejectedValueOnce(new Error('court busy'));
    // The self-run surface reports a refused write itself (it has no toast).
    const alert = vi.spyOn(window, 'alert').mockImplementation(() => {});
    const logError = vi.spyOn(console, 'error').mockImplementation(() => {});
    const recordScore = window.API.recordScore;
    try {
      await mount(write);
      const onSubmit = probe.props.onSubmit;
      let res;
      await act(async () => { res = await onSubmit({ status: 'running', winner: null }); });
      expect(res, 'a landed write').toBe(landed);
      await act(async () => { res = await onSubmit({ status: 'running', winner: null }); });
      expect(res, 'a refused write').toBeUndefined();
      expect(write).toHaveBeenCalledTimes(2);
    } finally {
      window.API.recordScore = recordScore;
      alert.mockRestore();
      logError.mockRestore();
    }
  });
});

// ── when an editor closes ────────────────────────────────────────────────────
// A start, each autosaved point and a kachinuki Record bout are running writes
// the operator keeps scoring after, so an editor that closed on every landed
// write dropped them out of the match after one point. And a write that did
// not land (queued offline, or superseded) must not look saved.

describe('the pools and bracket editors close only on a saved result (bc-plcl)', () => {
  const editorOpen = () => !!screen.queryByTestId('probe-score-editor');
  const submit = async (patch) => { await act(async () => { await probe.props.onSubmit(patch); }); };

  it('admin_pools.jsx stays open through running writes and a write that did not land', async () => {
    const write = vi.fn()
      .mockResolvedValueOnce({ status: 'running' })
      .mockResolvedValueOnce({ status: 'running' })
      .mockResolvedValueOnce({ queued: true })
      .mockResolvedValueOnce({ status: 'completed' });
    await mountPools(write);
    await submit({ status: 'running', winner: null });
    expect(editorOpen(), 'after Start').toBe(true);
    await submit({ status: 'running', ipponsA: ['M'] });
    expect(editorOpen(), 'after an autosaved point').toBe(true);
    await submit({ status: 'completed', winner: { id: 'p1', name: 'Yamada' } });
    expect(editorOpen(), 'after a finish that only queued').toBe(true);
    await submit({ status: 'completed', winner: { id: 'p1', name: 'Yamada' } });
    expect(editorOpen(), 'after a saved finish').toBe(false);
  });

  it('admin_competition_bracket.jsx keeps the editor on a correction that did not land', async () => {
    const bm = runningMatch({ id: 'bm1', status: 'completed', winner: { id: 'p1', name: 'Yamada' } });
    delete bm.phase;
    delete bm.poolName;
    window.BracketTree = (props) => (
      <button data-testid="open-bracket-match" onClick={() => props.onMatchClick(bm, 0, 0)}>open</button>
    );
    const write = vi.fn().mockResolvedValueOnce({ queued: true }).mockResolvedValueOnce({ status: 'completed' });
    await act(async () => {
      render(
        <AdminBracket
          c={{ id: 'c1', name: 'Comp', engi: false }}
          t={{ courts: ['A'] }}
          bracket={{ rounds: [[bm]] }}
          onMoveCourt={vi.fn()}
          onEditScore={write}
          tweaks={{}}
          password="pw"
        />
      );
    });
    await act(async () => { fireEvent.click(screen.getByTestId('open-bracket-match')); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Edit result' })); });
    expect(editorOpen(), 'the correction editor is open').toBe(true);
    await submit({ status: 'completed', winner: { id: 'p2', name: 'Tanaka' } });
    expect(editorOpen(), 'after a correction that only queued').toBe(true);
    await submit({ status: 'completed', winner: { id: 'p2', name: 'Tanaka' } });
    expect(editorOpen(), 'after a saved correction').toBe(false);
  });
});
