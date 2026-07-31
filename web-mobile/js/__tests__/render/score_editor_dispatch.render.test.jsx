// mp-yqxn.1: pin the ScoreEditorModal DISPATCH (admin_scoring_individual.jsx).
//
// window.ScoreEditorModal is a dispatcher with THREE terminal branches:
//   m.compEngi truthy                      → EngiScoreEditorModal
//   m.compKind === "team" || m.teamSize>0  → TeamScoreEditorModal
//   else                                   → the individual kendo editor
//
// These tests pin, for a given match shape, WHICH branch renders and WHICH
// props the dispatcher forwards to it. This is the cheapest guard against the
// class of bug already found twice here: a missing compEngi stamp routing an
// engi match to the ippon editor (mp-9k3v and its predecessor).
//
// REGRESSION-PIN ONLY (epic mp-yqxn constraint): these DOM assertions prove
// nothing about rendering, friction, or legibility. Every judgement call is
// owned by a browser-review child of mp-yqxn; where a pinned behaviour is
// probably wrong, the comment names the child that rules on it.

import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';

// Prop-recording probes for the two non-default branches. vi.mock factories
// are hoisted, so the recorders live in vi.hoisted state. The factory spreads
// importOriginal(): admin_scoring_modal.jsx also re-exports the team module's
// named helpers (teamResultLabel, isKoTieBlocked, ...) and those must keep
// resolving to the real implementations.
const probes = vi.hoisted(() => ({
  team: { props: null },
  engi: { props: null },
}));

vi.mock('../../admin_scoring_team.jsx', async (importOriginal) => {
  const mod = await importOriginal();
  return {
    ...mod,
    TeamScoreEditorModal: (props) => {
      probes.team.props = props;
      return React.createElement('div', { 'data-testid': 'probe-team-editor' });
    },
  };
});

vi.mock('../../admin_scoring_engi.jsx', async (importOriginal) => {
  const mod = await importOriginal();
  return {
    ...mod,
    EngiScoreEditorModal: (props) => {
      probes.engi.props = props;
      return React.createElement('div', { 'data-testid': 'probe-engi-editor' });
    },
  };
});

// Window globals required by admin_scoring_modal.jsx and its transitive
// imports (same set as admin_scoring_modal.render.test.jsx).
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
    recordScore: vi.fn(),
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

beforeEach(() => {
  probes.team.props = null;
  probes.engi.props = null;
});

// Base match: no compId so the config-fetch effect returns early.
function makeMatch(overrides = {}) {
  return {
    id: 'm1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ...overrides,
  };
}

// The full prop bag a mount site could wire. Forwarding gaps show up as
// undefined on the receiving probe.
function fullPropBag() {
  return {
    onClose: vi.fn(),
    onSubmit: vi.fn(),
    onSubmitAndNext: vi.fn(),
    onAfterDecision: vi.fn(),
    prevMatch: { id: 'pm' },
    nextMatch: { id: 'nm' },
    onPrev: vi.fn(),
    onNext: vi.fn(),
    password: 'pw',
    selfReport: true,
    variant: 'inline',
    canClose: false,
  };
}

function renderDispatch(match, props = {}) {
  return render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="" {...props} />);
}

describe('ScoreEditorModal dispatch: which branch renders', () => {
  it('compEngi: true routes to the engi editor', () => {
    renderDispatch(makeMatch({ compEngi: true }));
    expect(screen.getByTestId('probe-engi-editor')).toBeTruthy();
    expect(probes.team.props).toBeNull();
  });

  it('compEngi UNDEFINED (missing stamp — the real-world failure shape) routes an engi pair to the INDIVIDUAL ippon editor', () => {
    // This is the bug class found twice (mp-9k3v): a surface that forgets to
    // stamp compEngi sends a flag-scored engi pair to the ippon editor. The
    // dispatcher cannot recover a missing stamp; this pins that reality so
    // any new enrichment path that forgets the stamp fails visibly here
    // once someone strengthens the dispatch. Ruled on by mp-yqxn.6.
    const pairMatch = makeMatch({
      sideA: { id: 'p1', name: 'Aka One - Aka Two' },
      sideB: { id: 'p2', name: 'Shiro One - Shiro Two' },
    });
    expect('compEngi' in pairMatch).toBe(false);
    renderDispatch(pairMatch);
    expect(screen.queryByTestId('probe-engi-editor')).toBeNull();
    expect(screen.queryByTestId('probe-team-editor')).toBeNull();
    // Individual editor landmark: the modal root plus ippon buttons.
    expect(screen.getByTestId('scoring-modal-root')).toBeTruthy();
  });

  it('compEngi: false routes to the individual editor', () => {
    renderDispatch(makeMatch({ compEngi: false }));
    expect(screen.queryByTestId('probe-engi-editor')).toBeNull();
    expect(screen.getByTestId('scoring-modal-root')).toBeTruthy();
  });

  it('compKind: "team" routes to the team editor', () => {
    renderDispatch(makeMatch({ compKind: 'team', teamSize: 5, sideA: { id: 't1', name: 'Team A' }, sideB: { id: 't2', name: 'Team B' } }));
    expect(screen.getByTestId('probe-team-editor')).toBeTruthy();
    expect(probes.engi.props).toBeNull();
  });

  it('teamSize > 0 alone (compKind empty) still routes to the team editor', () => {
    // A team competition created with only teamSize set must not fall through
    // to the individual editor (dispatch comment, admin_scoring_individual.jsx).
    renderDispatch(makeMatch({ teamSize: 3, sideA: { id: 't1', name: 'Team A' }, sideB: { id: 't2', name: 'Team B' } }));
    expect(screen.getByTestId('probe-team-editor')).toBeTruthy();
  });

  it('compKind: "team" with teamSize 0 routes to the team editor (defaults to 5 positions)', () => {
    renderDispatch(makeMatch({ compKind: 'team', teamSize: 0, sideA: { id: 't1', name: 'Team A' }, sideB: { id: 't2', name: 'Team B' } }));
    expect(screen.getByTestId('probe-team-editor')).toBeTruthy();
    expect(probes.team.props.teamSize).toBe(5);
  });

  it('IMPOSSIBLE CELL — compEngi + team markers: engi wins (engi is checked first)', () => {
    // engi+team is rejected at creation and settings-PUT time
    // (handlers_competition.go: "engi is only valid for individual
    // competitions, not team"), so this state cannot be produced by the
    // product. Asserted rather than skipped: if it ever leaks in via legacy
    // data or a missed validation, the dispatch routes to the ENGI editor,
    // matching the server's "engi is never a team" rule.
    renderDispatch(makeMatch({ compEngi: true, compKind: 'team', teamSize: 5 }));
    expect(screen.getByTestId('probe-engi-editor')).toBeTruthy();
    expect(probes.team.props).toBeNull();
  });

  it('pool-daihyosen rep rows (compKind "", teamSize 0) route to the individual editor', () => {
    // viewer_utils.compMatches forces compKind="" AND teamSize=0 on rep bouts
    // so a team competition's tiebreaker bout is scored as ONE individual bout.
    renderDispatch(makeMatch({ compKind: '', teamSize: 0 }));
    expect(screen.queryByTestId('probe-team-editor')).toBeNull();
    expect(screen.getByTestId('scoring-modal-root')).toBeTruthy();
  });
});

describe('ScoreEditorModal dispatch: forwarded props per branch', () => {
  // Props BOTH non-default branches must forward identically. The three
  // admin/self-run props (password, selfReport, onAfterDecision) are asserted
  // per branch below because the branches deliberately DIFFER on them.
  const FORWARDED_TO_BOTH = [
    'onClose', 'onSubmit', 'onSubmitAndNext', 'prevMatch', 'nextMatch',
    'onPrev', 'onNext', 'variant', 'canClose',
  ];

  it('team branch forwards the FULL bag including password, selfReport and onAfterDecision', () => {
    const bag = fullPropBag();
    render(<ScoreEditorModal match={makeMatch({ compKind: 'team', teamSize: 5 })} {...bag} />);
    const p = probes.team.props;
    for (const k of FORWARDED_TO_BOTH) expect(p[k], k).toBe(bag[k]);
    expect(p.password).toBe('pw');
    expect(p.selfReport).toBe(true);
    expect(p.onAfterDecision).toBe(bag.onAfterDecision);
  });

  it('engi branch forwards navigation but DROPS password, selfReport and onAfterDecision', () => {
    // CURRENT behaviour, pinned: the engi branch forwards neither password
    // nor selfReport nor onAfterDecision (admin_scoring_individual.jsx).
    // Consequences: the engi editor cannot distinguish the public self-run
    // surface from the admin console, and a decision recorded from an engi
    // match cannot trigger the mount site's after-decision advance.
    // Whether that is correct is ruled on by mp-yqxn.6 (engi operator path)
    // and mp-yqxn.5 (public self-run surface). If a fix lands, flip these
    // expectations deliberately.
    const bag = fullPropBag();
    render(<ScoreEditorModal match={makeMatch({ compEngi: true })} {...bag} />);
    const p = probes.engi.props;
    for (const k of FORWARDED_TO_BOTH) expect(p[k], k).toBe(bag[k]);
    // The three dropped props:
    expect(p.password).toBeUndefined();
    expect(p.selfReport).toBeUndefined();
    expect(p.onAfterDecision).toBeUndefined();
  });
});
