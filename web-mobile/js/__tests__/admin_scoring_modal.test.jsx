import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  resolveDecisionPassword,
  buildDecisionBody,
  submitDecisionRequest,
  shouldShowEnchoMaxBanner,
  getIpponButtons,
  getValidPointKeys,
  canIncrementEncho,
  nextEnchoPeriod,
  prevEnchoPeriod,
  DecisionPrompt,
  MAX_IPPONS_PER_SIDE,
  isBoutDecided,
  applyFoulIncrement,
  reconcileFoulsAtOpen,
  nextFoulOnDecrement,
  applyFusenshoToggle,
  decideDrawToggle,
  shouldBlockScoringKeys,
} from '../admin_scoring_modal.jsx';
import { isKikenDecision } from '../api_serializers.jsx';

window.isKikenDecision = isKikenDecision;

// admin_scoring_modal.jsx ships seven module-private helpers that together
// implement the FR-033 encho-period flow (T104 cap + banner) and the
// T093–T098 decision prompt path. Each helper is exported separately so
// vitest can pin the behaviour without mounting either ScoreEditorModal /
// TeamScoreEditorModal — those run useState/useEffect at the top and the
// vitest.setup.js React mock stubs hooks, so component-level rendering
// tests would only ever see the initial render.

describe('resolveDecisionPassword', () => {
  // All ScoreEditorModal mount sites now pass password as an explicit prop.
  // resolveDecisionPassword returns the prop directly (or "" as a sentinel).

  it('returns the explicit prop when present', () => {
    expect(resolveDecisionPassword('prop-password')).toBe('prop-password');
  });

  it('returns "" when prop is empty/missing (server will 401 — misconfiguration)', () => {
    expect(resolveDecisionPassword('')).toBe('');
    expect(resolveDecisionPassword(undefined)).toBe('');
    expect(resolveDecisionPassword(null)).toBe('');
  });
});

describe('buildDecisionBody', () => {
  // T093/T094: the /decision POST body. Pin the wire shape against the
  // server contract in handlers_decision.go: { decision, decisionBy,
  // decisionReason?, encho?: { periodCount }, force? }.

  it('builds the minimal body for kiken-voluntary without reason or encho', () => {
    const body = buildDecisionBody('kiken-voluntary', { decisionBy: 'shiro', decisionReason: '' }, 0);
    expect(body).toEqual({ decision: 'kiken-voluntary', decisionBy: 'shiro' });
  });

  it('builds the minimal body for kiken-injury without reason or encho', () => {
    const body = buildDecisionBody('kiken-injury', { decisionBy: 'aka', decisionReason: '' }, 0);
    expect(body).toEqual({ decision: 'kiken-injury', decisionBy: 'aka' });
  });

  it('includes decisionReason when present', () => {
    const body = buildDecisionBody('kiken-voluntary', { decisionBy: 'aka', decisionReason: 'injury' }, 0);
    expect(body).toEqual({
      decision: 'kiken-voluntary',
      decisionBy: 'aka',
      decisionReason: 'injury',
    });
  });

  it('omits decisionReason when empty string', () => {
    // The server distinguishes "no reason given" from "empty reason
    // intentional" via omission; an empty string is the same as
    // omission in our wire contract.
    const body = buildDecisionBody('fusenpai', { decisionBy: 'shiro', decisionReason: '' }, 0);
    expect(body).not.toHaveProperty('decisionReason');
  });

  it('attaches encho.periodCount when > 0', () => {
    const body = buildDecisionBody('hikiwake', { decisionBy: 'shiro' }, 2);
    expect(body.encho).toEqual({ periodCount: 2 });
  });

  it('omits encho when periodCount is 0', () => {
    const body = buildDecisionBody('kiken-voluntary', { decisionBy: 'shiro' }, 0);
    expect(body).not.toHaveProperty('encho');
  });

  it('omits encho when periodCount is negative or NaN (defensive)', () => {
    // Shouldn't happen in practice (the UI clamps at 0), but pinning
    // the > 0 check so a future refactor that uses `!= 0` doesn't
    // silently re-introduce negative counts.
    expect(buildDecisionBody('kiken-voluntary', { decisionBy: 'shiro' }, -1)).not.toHaveProperty('encho');
    expect(buildDecisionBody('kiken-voluntary', { decisionBy: 'shiro' }, NaN)).not.toHaveProperty('encho');
  });

  it('passes kind through verbatim (no validation here)', () => {
    // The helper is dumb — the parent decides which kinds are legal.
    // Wire validation happens server-side via DecisionRequest.Validate.
    const body = buildDecisionBody('daihyosen', { decisionBy: 'aka' }, 1);
    expect(body.decision).toBe('daihyosen');
  });

  describe('force flag (T103/T104 override loop)', () => {
    // T103 (decision_locked) and T104 (max_encho_exceeded) both use a
    // confirm-and-retry-with-force flow. The helper accepts `opts.force`
    // so the parent's retry path can call back into it without
    // re-implementing the body shape.

    it('attaches force=true when opts.force is set', () => {
      const body = buildDecisionBody('kiken-voluntary', { decisionBy: 'shiro' }, 0, { force: true });
      expect(body.force).toBe(true);
    });

    it('omits force when opts.force is missing or false', () => {
      expect(buildDecisionBody('kiken-voluntary', { decisionBy: 'shiro' }, 0)).not.toHaveProperty('force');
      expect(buildDecisionBody('kiken-voluntary', { decisionBy: 'shiro' }, 0, {})).not.toHaveProperty('force');
      expect(buildDecisionBody('kiken-voluntary', { decisionBy: 'shiro' }, 0, { force: false })).not.toHaveProperty('force');
    });

    it('force combines with reason + encho cleanly', () => {
      const body = buildDecisionBody(
        'kiken-voluntary',
        { decisionBy: 'aka', decisionReason: 'no-show' },
        3,
        { force: true },
      );
      expect(body).toEqual({
        decision: 'kiken-voluntary',
        decisionBy: 'aka',
        decisionReason: 'no-show',
        encho: { periodCount: 3 },
        force: true,
      });
    });
  });
});

describe('shouldShowEnchoMaxBanner (T104)', () => {
  // The "Maximum encho periods reached" banner surfaces once the operator
  // has incremented to the cap. maxEnchoPeriods === 0 means "unlimited"
  // per state.CompetitionConfig.MaxEnchoPeriods's FIK-default semantics.

  it('returns false when maxEnchoPeriods is 0 (unlimited)', () => {
    expect(shouldShowEnchoMaxBanner(0, 0)).toBe(false);
    expect(shouldShowEnchoMaxBanner(5, 0)).toBe(false);
    expect(shouldShowEnchoMaxBanner(100, 0)).toBe(false);
  });

  it('returns false when maxEnchoPeriods is null/undefined (unlimited)', () => {
    // Defensive: a missing field on the wire shouldn't surprise the
    // operator with a banner — treat as unlimited.
    expect(shouldShowEnchoMaxBanner(5, null)).toBe(false);
    expect(shouldShowEnchoMaxBanner(5, undefined)).toBe(false);
  });

  it('returns false when enchoPeriodCount is below the cap', () => {
    expect(shouldShowEnchoMaxBanner(0, 3)).toBe(false);
    expect(shouldShowEnchoMaxBanner(1, 3)).toBe(false);
    expect(shouldShowEnchoMaxBanner(2, 3)).toBe(false);
  });

  it('returns true when enchoPeriodCount equals the cap (at-cap warning)', () => {
    expect(shouldShowEnchoMaxBanner(3, 3)).toBe(true);
    expect(shouldShowEnchoMaxBanner(1, 1)).toBe(true);
  });

  it('returns true when enchoPeriodCount exceeds the cap (defensive)', () => {
    // The + button is clamped, so this is unreachable through the UI —
    // but pinning the `>=` so a future refactor doesn't accidentally
    // narrow to `===` and leave operators staring at a non-existent
    // banner if the count somehow over-shoots.
    expect(shouldShowEnchoMaxBanner(4, 3)).toBe(true);
    expect(shouldShowEnchoMaxBanner(10, 3)).toBe(true);
  });

  it('returns false when maxEnchoPeriods is negative (defensive)', () => {
    // Negative cap is meaningless — treat as unlimited rather than
    // banner-on-everything.
    expect(shouldShowEnchoMaxBanner(5, -1)).toBe(false);
  });
});

describe('canIncrementEncho (T104 + button gate)', () => {
  it('returns true when maxEnchoPeriods is 0 (unlimited)', () => {
    expect(canIncrementEncho(0, 0)).toBe(true);
    expect(canIncrementEncho(100, 0)).toBe(true);
  });

  it('returns true when maxEnchoPeriods is null/undefined', () => {
    expect(canIncrementEncho(5, null)).toBe(true);
    expect(canIncrementEncho(5, undefined)).toBe(true);
  });

  it('returns true when below the cap', () => {
    expect(canIncrementEncho(0, 3)).toBe(true);
    expect(canIncrementEncho(2, 3)).toBe(true);
  });

  it('returns false when at the cap', () => {
    expect(canIncrementEncho(3, 3)).toBe(false);
  });

  it('returns false when above the cap (defensive)', () => {
    expect(canIncrementEncho(4, 3)).toBe(false);
  });
});

describe('nextEnchoPeriod (T104 + button clamp)', () => {
  // The + button uses this helper: increments by 1 but clamps at the
  // configured cap. Combined with disabled={!canIncrementEncho(...)}
  // the button can't fire over-the-cap, but pinning the clamp here so
  // a future refactor that drops the disabled gate doesn't silently
  // let the count run away.

  it('increments by 1 when unlimited (maxEnchoPeriods = 0)', () => {
    expect(nextEnchoPeriod(1, 0)).toBe(2);
    expect(nextEnchoPeriod(99, 0)).toBe(100);
  });

  it('increments by 1 when below the cap', () => {
    expect(nextEnchoPeriod(1, 3)).toBe(2);
    expect(nextEnchoPeriod(2, 3)).toBe(3);
  });

  it('does NOT exceed the cap (clamps at max)', () => {
    expect(nextEnchoPeriod(3, 3)).toBe(3);
    expect(nextEnchoPeriod(5, 3)).toBe(5); // already over → stays put
  });
});

describe('prevEnchoPeriod (the − button)', () => {
  // The − button uses Math.max(1, current - 1) — clamps at 1 because
  // the encho toggle (above the +/-) is the "set to 0" path. Pinning
  // the clamp so the count can't slip below 1 once the toggle is on.

  it('decrements by 1', () => {
    expect(prevEnchoPeriod(5)).toBe(4);
    expect(prevEnchoPeriod(2)).toBe(1);
  });

  it('clamps at 1 (does not drop to 0)', () => {
    expect(prevEnchoPeriod(1)).toBe(1);
    expect(prevEnchoPeriod(0)).toBe(1); // defensive: already off, still floor=1
  });

  it('clamps negative values to 1 (defensive)', () => {
    expect(prevEnchoPeriod(-3)).toBe(1);
  });
});

describe('DecisionPrompt → /decision POST integration', () => {
  // The component itself only calls its `onSubmit` prop with
  // { decisionBy, decisionReason }; the parent ScoreEditorModal's
  // submitDecision then builds the body + resolves the password +
  // calls window.API.recordDecision. The integration test wires the
  // helpers together to pin the full flow against the server contract.

  let originalAPI;
  beforeEach(() => {
    originalAPI = window.API;
    window.API = {
      recordDecision: vi.fn().mockResolvedValue({ winner: 'Tora', sideA: 'Tora', sideB: 'Kuma' }),
    };
  });
  afterEach(() => {
    window.API = originalAPI;
  });

  it('DecisionPrompt onSubmit fires the form-submit handler with default side', () => {
    // The React mock returns `[initial, vi.fn()]` from useState, so
    // calling DecisionPrompt as a function produces the initial-state
    // virtual tree; the form's onSubmit is what we exercise here.
    const onSubmit = vi.fn();
    const tree = DecisionPrompt({
      kind: 'kiken',
      sideA: { name: 'Tora' },
      sideB: { name: 'Kuma' },
      defaultSide: 'shiro',
      askReason: true,
      onCancel: vi.fn(),
      onSubmit,
      submitting: false,
    });
    expect(tree.type).toBe('form');
    expect(typeof tree.props.onSubmit).toBe('function');

    tree.props.onSubmit({ preventDefault: () => {} });
    expect(onSubmit).toHaveBeenCalledWith({
      decisionBy: 'shiro',
      decisionReason: '',
    });
  });

  it('DecisionPrompt onSubmit defaults side to "shiro" when defaultSide is missing', () => {
    const onSubmit = vi.fn();
    const tree = DecisionPrompt({
      kind: 'fusenpai',
      sideA: { name: 'Tora' },
      sideB: { name: 'Kuma' },
      askReason: false,
      onCancel: vi.fn(),
      onSubmit,
      submitting: false,
    });
    tree.props.onSubmit({ preventDefault: () => {} });
    expect(onSubmit).toHaveBeenCalledWith({
      decisionBy: 'shiro',
      decisionReason: '',
    });
  });

  it('DecisionPrompt onSubmit is a no-op while submitting', () => {
    // Guards against double-submit when the operator double-clicks
    // the Record button.
    const onSubmit = vi.fn();
    const tree = DecisionPrompt({
      kind: 'kiken',
      sideA: { name: 'Tora' },
      sideB: { name: 'Kuma' },
      defaultSide: 'aka',
      askReason: false,
      onCancel: vi.fn(),
      onSubmit,
      submitting: true,
    });
    tree.props.onSubmit({ preventDefault: () => {} });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('the parent flow: DecisionPrompt onSubmit → submitDecisionRequest → recordDecision', async () => {
    // Route the DecisionPrompt callback through submitDecisionRequest —
    // the same path ScoreEditorModal.submitDecision takes — so the test
    // would fail if the password stopped flowing to recordDecision.
    const onSubmit = vi.fn((payload) =>
      submitDecisionRequest('comp-1', 'match-1', 'kiken-voluntary', payload, 0, 'explicit-pw'),
    );

    const tree = DecisionPrompt({
      kind: 'kiken',
      sideA: { name: 'Tora' },
      sideB: { name: 'Kuma' },
      defaultSide: 'aka',
      askReason: true,
      onCancel: vi.fn(),
      onSubmit,
      submitting: false,
    });

    await tree.props.onSubmit({ preventDefault: () => {} });

    expect(window.API.recordDecision).toHaveBeenCalledWith(
      'comp-1',
      'match-1',
      { decision: 'kiken-voluntary', decisionBy: 'aka' },
      'explicit-pw',
    );
  });

  it('regression: submitDecision path forwards the modal password prop to recordDecision', async () => {
    // This is the production path used by ScoreEditorModal/TeamScoreEditorModal.
    await submitDecisionRequest(
      'comp-9',
      'match-9',
      'fusenpai',
      { decisionBy: 'shiro', decisionReason: '' },
      0,
      'tournament-secret',
    );
    expect(window.API.recordDecision).toHaveBeenCalledWith(
      'comp-9',
      'match-9',
      { decision: 'fusenpai', decisionBy: 'shiro' },
      'tournament-secret',
    );
  });

  it('parent flow includes encho.periodCount in the body when > 0', async () => {
    // The encho counter rides alongside the decision so the server can
    // attach periodCount to MatchResult.Encho. Pinned here so the
    // wiring through buildDecisionBody isn't dropped during a refactor.
    const onSubmit = vi.fn((payload) => {
      const body = buildDecisionBody('hikiwake', payload, 3); // 3 encho periods
      const password = resolveDecisionPassword('pw');
      return window.API.recordDecision('comp-1', 'match-1', body, password);
    });
    const tree = DecisionPrompt({
      kind: 'hikiwake',
      sideA: { name: 'A' },
      sideB: { name: 'B' },
      defaultSide: 'shiro',
      askReason: false,
      onCancel: vi.fn(),
      onSubmit,
      submitting: false,
    });

    await tree.props.onSubmit({ preventDefault: () => {} });

    expect(window.API.recordDecision).toHaveBeenCalledWith(
      'comp-1',
      'match-1',
      { decision: 'hikiwake', decisionBy: 'shiro', encho: { periodCount: 3 } },
      'pw',
    );
  });

  it('parent flow attaches force=true on the retry-after-409 path', async () => {
    // T103/T104: when the server replies decision_locked or
    // max_encho_exceeded the parent's submitDecision recurses with
    // { force: true } after the operator confirms. The body must carry
    // that flag through to the server so the second attempt isn't
    // also rejected.
    const onSubmit = vi.fn((payload) => {
      const body = buildDecisionBody('kiken-voluntary', payload, 0, { force: true });
      const password = resolveDecisionPassword('pw');
      return window.API.recordDecision('comp-1', 'match-1', body, password);
    });
    const tree = DecisionPrompt({
      kind: 'kiken',
      sideA: { name: 'A' },
      sideB: { name: 'B' },
      defaultSide: 'shiro',
      askReason: false,
      onCancel: vi.fn(),
      onSubmit,
      submitting: false,
    });
    await tree.props.onSubmit({ preventDefault: () => {} });
    expect(window.API.recordDecision).toHaveBeenCalledWith(
      'comp-1',
      'match-1',
      { decision: 'kiken-voluntary', decisionBy: 'shiro', force: true },
      'pw',
    );
  });

  it('fusenpai: decisionBy is the ABSENT/LOSING side, not the winning side', () => {
    // The UI label was previously "Which side gets the default win?" —
    // operators interpreted it as picking the WINNER and sent the wrong
    // side as decisionBy, inverting the result. The label is now
    // "Which side did not show up?" so operators pick the ABSENT (losing)
    // side. This test pins the wire contract: selecting "shiro" means
    // SHIRO forfeits and AKA receives the auto-filled 2-0 win.
    const onSubmit = vi.fn((payload) => {
      return buildDecisionBody('fusenpai', payload, 0);
    });
    const tree = DecisionPrompt({
      kind: 'fusenpai',
      sideA: { name: 'Hayashi' },
      sideB: { name: 'Nakamura' },
      defaultSide: 'shiro',
      askReason: false,
      onCancel: vi.fn(),
      onSubmit,
      submitting: false,
    });
    tree.props.onSubmit({ preventDefault: () => {} });
    // decisionBy = "shiro" → SHIRO is the absent side → engine gives win to AKA
    expect(onSubmit).toHaveBeenCalledWith({ decisionBy: 'shiro', decisionReason: '' });
    const body = buildDecisionBody('fusenpai', { decisionBy: 'shiro', decisionReason: '' }, 0);
    expect(body).toEqual({ decision: 'fusenpai', decisionBy: 'shiro' });
  });
});

describe('+ / − encho button behaviour (T104 clamp invariants)', () => {
  // End-to-end pin: the clamp on the + button must not let the count
  // exceed maxEnchoPeriods even across repeated clicks, and the − button
  // must not drop below 1. These compose the canIncrementEncho /
  // nextEnchoPeriod / prevEnchoPeriod helpers.

  it('+ button: repeated clicks stop at the cap', () => {
    let count = 1;
    const maxEnchoPeriods = 3;
    for (let i = 0; i < 10; i++) {
      count = nextEnchoPeriod(count, maxEnchoPeriods);
    }
    expect(count).toBe(3);
  });

  it('+ button: when unlimited (max=0), repeated clicks keep climbing', () => {
    let count = 1;
    for (let i = 0; i < 5; i++) {
      count = nextEnchoPeriod(count, 0);
    }
    expect(count).toBe(6);
  });

  it('− button: repeated clicks bottom out at 1, not 0', () => {
    let count = 3;
    for (let i = 0; i < 10; i++) {
      count = prevEnchoPeriod(count);
    }
    expect(count).toBe(1);
  });

  it('the +/− pair is symmetric within the [1, max] window', () => {
    const maxEnchoPeriods = 5;
    let count = 1;
    // Climb to cap
    count = nextEnchoPeriod(count, maxEnchoPeriods); // 2
    count = nextEnchoPeriod(count, maxEnchoPeriods); // 3
    count = nextEnchoPeriod(count, maxEnchoPeriods); // 4
    count = nextEnchoPeriod(count, maxEnchoPeriods); // 5
    expect(count).toBe(5);
    expect(canIncrementEncho(count, maxEnchoPeriods)).toBe(false);
    // Step down to floor
    count = prevEnchoPeriod(count); // 4
    count = prevEnchoPeriod(count); // 3
    count = prevEnchoPeriod(count); // 2
    count = prevEnchoPeriod(count); // 1
    expect(count).toBe(1);
    // Try to go below
    count = prevEnchoPeriod(count); // still 1
    expect(count).toBe(1);
  });
});

describe('isBoutDecided / MAX_IPPONS_PER_SIDE', () => {
  // Kendo best-of-3: once either side reaches 2 ippons the bout ends.
  // isBoutDecided drives the disabled-prop on ippon-add buttons in both
  // ScoreEditorModal and TeamScoreEditorModal — once it returns true, all
  // M/K/D/T/H buttons on BOTH sides are disabled, preventing a 2-2 entry.
  // Server-side mirror: validateIpponCounts in internal/mobileapp/validation.go.

  it('exports MAX_IPPONS_PER_SIDE = 2', () => {
    expect(MAX_IPPONS_PER_SIDE).toBe(2);
  });

  it('returns false when both sides are empty (match not yet started)', () => {
    expect(isBoutDecided([], [])).toBe(false);
  });

  it('returns false while both sides are below the cap', () => {
    expect(isBoutDecided([], ['M'])).toBe(false);
    expect(isBoutDecided(['K'], [])).toBe(false);
    expect(isBoutDecided(['M'], ['K'])).toBe(false);  // 1-1 — valid, ongoing
  });

  it('returns true when side A reaches 2 ippons (decisive win for A)', () => {
    expect(isBoutDecided(['M', 'K'], [])).toBe(true);   // 2-0
    expect(isBoutDecided(['M', 'K'], ['D'])).toBe(true); // 2-1
  });

  it('returns true when side B reaches 2 ippons (decisive win for B)', () => {
    expect(isBoutDecided([], ['M', 'K'])).toBe(true);   // 0-2
    expect(isBoutDecided(['D'], ['M', 'K'])).toBe(true); // 1-2
  });

  it('returns true for 2-2 (the impossible scoreline the fix prevents)', () => {
    // This mirrors the server-side rejection in validateIpponCounts:
    //   "both sides cannot have 2 ippons (best-of-3 ends at first to 2)"
    expect(isBoutDecided(['M', 'K'], ['D', 'T'])).toBe(true);
  });

  it('handles undefined/null gracefully (defensive — arrays may be initialised late)', () => {
    expect(isBoutDecided(undefined, [])).toBe(false);
    expect(isBoutDecided([], null)).toBe(false);
    expect(isBoutDecided(undefined, undefined)).toBe(false);
  });

  it('returns false after removing an ippon that previously hit the cap', () => {
    // Simulate operator hitting 2 then clicking a slot to remove one:
    // boutDecided re-evaluates on next render with the shorter array.
    const after = ['M']; // was ['M','K'], operator removed 'K'
    expect(isBoutDecided(after, ['D'])).toBe(false); // back to 1-1, not decided
  });
});

describe('applyFoulIncrement (FIK 2-foul auto-award)', () => {
  // Per FIK rules and the project's own glossary (`internal/domain/glossary.go`):
  // "Two hansoku awarded to a competitor give the opponent one free point."
  // applyFoulIncrement models a single `+` press on a side's foul counter:
  // the 2nd press discharges into a hansoku ippon ("H") for the OPPONENT and
  // resets this side's counter to 0. The 1st press just increments by 1.

  it('first foul increments to 1, opponent untouched', () => {
    expect(applyFoulIncrement(0, [])).toEqual({ fouls: 1, opponentPts: [] });
  });

  it('second foul auto-awards H to opponent and resets counter', () => {
    expect(applyFoulIncrement(1, [])).toEqual({ fouls: 0, opponentPts: ['H'] });
  });

  it('second foul appends H to existing opponent ippons', () => {
    // Opponent already has 1 ippon — H is appended into the second slot.
    expect(applyFoulIncrement(1, ['M'])).toEqual({ fouls: 0, opponentPts: ['M', 'H'] });
  });

  it('second foul is a no-op when opponent slot is already full', () => {
    // Best-of-3 cap: with 2 ippons the bout is already decided. Swallow
    // the extra foul rather than crash. The counter still resets to 0.
    expect(applyFoulIncrement(1, ['M', 'K'])).toEqual({ fouls: 0, opponentPts: ['M', 'K'] });
  });

  it('four sequential presses produce 2 H in opponent slots, fouls=0', () => {
    // Simulate the operator clicking `+` four times in a row. After:
    //   press 1: fouls=1, opp=[]
    //   press 2: fouls=0, opp=['H']
    //   press 3: fouls=1, opp=['H']
    //   press 4: fouls=0, opp=['H','H']   ← bout now decided
    let state = { fouls: 0, opponentPts: [] };
    state = applyFoulIncrement(state.fouls, state.opponentPts);
    expect(state).toEqual({ fouls: 1, opponentPts: [] });
    state = applyFoulIncrement(state.fouls, state.opponentPts);
    expect(state).toEqual({ fouls: 0, opponentPts: ['H'] });
    state = applyFoulIncrement(state.fouls, state.opponentPts);
    expect(state).toEqual({ fouls: 1, opponentPts: ['H'] });
    state = applyFoulIncrement(state.fouls, state.opponentPts);
    expect(state).toEqual({ fouls: 0, opponentPts: ['H', 'H'] });
  });

  it('respects custom maxIppons cap (defensive)', () => {
    // maxIppons param is the same MAX_IPPONS_PER_SIDE default — pinned
    // here so a future refactor that lifts the cap doesn't silently let
    // 3+ H pile up in the opponent's slot.
    expect(applyFoulIncrement(1, ['M'], [], 1)).toEqual({ fouls: 0, opponentPts: ['M'] });
  });

  it('second foul is a no-op when THIS side is already at maxIppons', () => {
    // Bout-decided guard: if THIS side reached 2 ippons (bout already
    // won by THIS side), the 2nd foul cannot auto-award an H to opp
    // without producing an invalid 2-2 scoreline that the server's
    // validateIpponCounts would reject. Counter still resets to 0;
    // opponent's pts are left untouched.
    expect(applyFoulIncrement(1, ['X'], ['M', 'K'])).toEqual({ fouls: 0, opponentPts: ['X'] });
    expect(applyFoulIncrement(1, [], ['M', 'K'])).toEqual({ fouls: 0, opponentPts: [] });
  });

  it('second foul is a no-op when BOTH sides are at maxIppons (already 2-2 is impossible but defensive)', () => {
    // Backstop for a hypothetical corrupted state — the function must
    // not push a 3rd ippon onto opp regardless of how it was called.
    expect(applyFoulIncrement(1, ['M', 'K'], ['M', 'K'])).toEqual({ fouls: 0, opponentPts: ['M', 'K'] });
  });
});

describe('reconcileFoulsAtOpen (correction-flow normalization)', () => {
  // Reopen/correction flow: pre-fix builds stored hansoku as a cumulative
  // raw count (0..N) and folded floor(N/2) "H" entries into the opponent's
  // ippon array at submit. The post-fix counter is "outstanding fouls not
  // yet discharged" (0 or 1). Naively stripping rawFouls % 2 is correct
  // when the H's are already in opp's pts; it silently loses points when
  // they're not (legacy/imported data). reconcileFoulsAtOpen tops up the
  // missing H's before returning the remainder.

  it('passes through when no fouls', () => {
    expect(reconcileFoulsAtOpen(0, [])).toEqual({ outstandingFouls: 0, opponentPts: [] });
    expect(reconcileFoulsAtOpen(0, ['M'])).toEqual({ outstandingFouls: 0, opponentPts: ['M'] });
  });

  it('rawFouls=1 has no discharged H, just 1 outstanding', () => {
    expect(reconcileFoulsAtOpen(1, [])).toEqual({ outstandingFouls: 1, opponentPts: [] });
    expect(reconcileFoulsAtOpen(1, ['M'])).toEqual({ outstandingFouls: 1, opponentPts: ['M'] });
  });

  it('idempotent when the discharged H is already present (consistent prior data)', () => {
    // rawFouls=2 → expected 1 H. Opp already has it from the old buildPatch fold.
    expect(reconcileFoulsAtOpen(2, ['H'])).toEqual({ outstandingFouls: 0, opponentPts: ['H'] });
    // Mixed manual + derived: opp already has 1 manual + 1 derived = 2 total.
    expect(reconcileFoulsAtOpen(2, ['M', 'H'])).toEqual({ outstandingFouls: 0, opponentPts: ['M', 'H'] });
  });

  it('tops up missing H when legacy/imported data has fouls but no fold', () => {
    // rawFouls=2 with empty opp pts (corrupted/imported) — top up 1 H.
    expect(reconcileFoulsAtOpen(2, [])).toEqual({ outstandingFouls: 0, opponentPts: ['H'] });
    // Manual ippon exists but the foul-derived H was never folded in.
    expect(reconcileFoulsAtOpen(2, ['M'])).toEqual({ outstandingFouls: 0, opponentPts: ['M', 'H'] });
  });

  it('handles odd cumulative counts: floor pair discharges, remainder outstanding', () => {
    // rawFouls=3 → 1 H discharged, 1 outstanding.
    expect(reconcileFoulsAtOpen(3, [])).toEqual({ outstandingFouls: 1, opponentPts: ['H'] });
    expect(reconcileFoulsAtOpen(3, ['H'])).toEqual({ outstandingFouls: 1, opponentPts: ['H'] });
  });

  it('caps the top-up at maxIppons (bout would have ended)', () => {
    // rawFouls=4 → expected 2 H. Opp slot capped at 2 — top up fills the slot.
    expect(reconcileFoulsAtOpen(4, [])).toEqual({ outstandingFouls: 0, opponentPts: ['H', 'H'] });
    // Opp already has 1 ippon, so only room for 1 H top-up despite expecting 2.
    expect(reconcileFoulsAtOpen(4, ['M'])).toEqual({ outstandingFouls: 0, opponentPts: ['M', 'H'] });
  });

  it('does not duplicate when opp already has more H than expected', () => {
    // expected 1 H from fouls, but opp has 2 H's (e.g., two manual H awards).
    // Don't add — opp's count exceeds the expected fold count.
    expect(reconcileFoulsAtOpen(2, ['H', 'H'])).toEqual({ outstandingFouls: 0, opponentPts: ['H', 'H'] });
  });

  it('respects custom maxIppons cap', () => {
    expect(reconcileFoulsAtOpen(4, [], 1)).toEqual({ outstandingFouls: 0, opponentPts: ['H'] });
  });

  it('defensive: clamps negative rawFouls to 0', () => {
    expect(reconcileFoulsAtOpen(-1, ['M'])).toEqual({ outstandingFouls: 0, opponentPts: ['M'] });
  });
});

describe('nextFoulOnDecrement (team `−` button regression)', () => {
  // Pre-existing bug: the team sub-match `−` button passed a React-style
  // functional updater (`f => Math.max(0, f - 1)`) to `rs.setFouls`, but
  // rs.setFouls is a plain setter that wrote the function itself into
  // bFouls — breaking comparisons and rendering. Extracted to a pure
  // helper so the contract (returns a NUMBER, not a function) is
  // directly testable.

  it('returns a number, not a function (regression guard)', () => {
    expect(typeof nextFoulOnDecrement(2)).toBe('number');
    expect(typeof nextFoulOnDecrement(0)).toBe('number');
  });

  it('decrements by 1', () => {
    expect(nextFoulOnDecrement(1)).toBe(0);
    expect(nextFoulOnDecrement(2)).toBe(1);
    expect(nextFoulOnDecrement(3)).toBe(2);
  });

  it('clamps at 0 (never returns negative)', () => {
    expect(nextFoulOnDecrement(0)).toBe(0);
    expect(nextFoulOnDecrement(-1)).toBe(0);
  });
});

describe('applyFusenshoToggle', () => {
  // Per-bout Fusensho is a toggle in TeamScoreEditorModal. Toggle-on
  // overwrites the bout to a 2-0 default win for the chosen side; the
  // pre-fusensho points are stashed in _preFusensho so that toggling off
  // (re-clicking the active side) can restore them. Bug fix: previously
  // the untoggle only cleared the flag and left the auto-filled 2-0 in
  // place, losing the operator's prior score.

  const clean = () => ({ aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: "" });

  it('toggle-on from a clean state captures the snapshot and writes 2-0 for A', () => {
    const prev = { aPts: ['M'], bPts: [], aFouls: 0, bFouls: 0, fusensho: "" };
    const next = applyFusenshoToggle(prev, "a");
    expect(next).toEqual({
      aPts: ['M', 'M'],
      bPts: [],
      aFouls: 0,
      bFouls: 0,
      fusensho: "a",
      _preFusensho: { aPts: ['M'], bPts: [], aFouls: 0, bFouls: 0 },
    });
  });

  it('toggle-on from a clean state captures the snapshot and writes 2-0 for B', () => {
    const prev = { aPts: [], bPts: ['K'], aFouls: 1, bFouls: 0, fusensho: "" };
    const next = applyFusenshoToggle(prev, "b");
    expect(next).toEqual({
      aPts: [],
      bPts: ['M', 'M'],
      aFouls: 0,
      bFouls: 0,
      fusensho: "b",
      _preFusensho: { aPts: [], bPts: ['K'], aFouls: 1, bFouls: 0 },
    });
  });

  it('toggle-off (re-click active side) restores the snapshot exactly', () => {
    // Round trip: scored 1-0 SHIRO, toggled fusensho A, untoggle should
    // restore the 1-0 instead of leaving the 2-0 in place.
    const initial = { aPts: ['M'], bPts: [], aFouls: 0, bFouls: 0, fusensho: "" };
    const afterOn = applyFusenshoToggle(initial, "a");
    const afterOff = applyFusenshoToggle(afterOn, "a");
    expect(afterOff).toEqual({
      aPts: ['M'],
      bPts: [],
      aFouls: 0,
      bFouls: 0,
      fusensho: "",
      _preFusensho: undefined,
    });
  });

  it('side-switch preserves the original snapshot, untoggle then restores genuine pre-fusensho state', () => {
    // 1-0 SHIRO → toggle fusensho A (2-0 A) → toggle fusensho B (2-0 B)
    // → untoggle fusensho B should restore the original 1-0, NOT zeros
    // and NOT the intermediate 2-0 for A.
    const initial = { aPts: ['M'], bPts: [], aFouls: 0, bFouls: 0, fusensho: "" };
    const afterA = applyFusenshoToggle(initial, "a");
    expect(afterA.fusensho).toBe("a");
    expect(afterA._preFusensho).toEqual({ aPts: ['M'], bPts: [], aFouls: 0, bFouls: 0 });

    const afterSwitch = applyFusenshoToggle(afterA, "b");
    expect(afterSwitch.fusensho).toBe("b");
    expect(afterSwitch.aPts).toEqual([]);
    expect(afterSwitch.bPts).toEqual(['M', 'M']);
    // Snapshot stays anchored to the genuine pre-fusensho state.
    expect(afterSwitch._preFusensho).toEqual({ aPts: ['M'], bPts: [], aFouls: 0, bFouls: 0 });

    const afterUntoggleB = applyFusenshoToggle(afterSwitch, "b");
    expect(afterUntoggleB).toEqual({
      aPts: ['M'],
      bPts: [],
      aFouls: 0,
      bFouls: 0,
      fusensho: "",
      _preFusensho: undefined,
    });
  });

  it('fresh-match round-trip: zeros → toggle → untoggle returns to zeros', () => {
    const afterOn = applyFusenshoToggle(clean(), "a");
    expect(afterOn.aPts).toEqual(['M', 'M']);
    expect(afterOn._preFusensho).toEqual({ aPts: [], bPts: [], aFouls: 0, bFouls: 0 });
    const afterOff = applyFusenshoToggle(afterOn, "a");
    expect(afterOff).toEqual({
      aPts: [],
      bPts: [],
      aFouls: 0,
      bFouls: 0,
      fusensho: "",
      _preFusensho: undefined,
    });
  });

  it('defensive: untoggle without a snapshot just clears the flag', () => {
    // Models the modal-reopen case: initSubs reads decision="fusensho"
    // from the backend payload and lights up the button, but does NOT
    // round-trip the snapshot. Untoggling in that state must not crash;
    // it falls through to clearing the flag and leaving the score alone.
    const prev = { aPts: ['M', 'M'], bPts: [], aFouls: 0, bFouls: 0, fusensho: "a" };
    const next = applyFusenshoToggle(prev, "a");
    expect(next).toEqual({
      aPts: ['M', 'M'],
      bPts: [],
      aFouls: 0,
      bFouls: 0,
      fusensho: "",
      _preFusensho: undefined,
    });
  });
});

describe('getIpponButtons', () => {
  it('returns Kendo set (no S) when isNaginata is false', () => {
    expect(getIpponButtons(false)).toEqual(["M", "K", "D", "T", "H"]);
  });

  it('returns Kendo set when isNaginata is falsy (undefined)', () => {
    expect(getIpponButtons(undefined)).toEqual(["M", "K", "D", "T", "H"]);
  });

  it('returns Naginata set (with S before H) when isNaginata is true', () => {
    expect(getIpponButtons(true)).toEqual(["M", "K", "D", "T", "S", "H"]);
  });

  it('Naginata set has exactly one extra entry vs Kendo set', () => {
    expect(getIpponButtons(true)).toHaveLength(getIpponButtons(false).length + 1);
  });

  it('S appears between T and H in the Naginata set', () => {
    const btns = getIpponButtons(true);
    const tIdx = btns.indexOf("T");
    const sIdx = btns.indexOf("S");
    const hIdx = btns.indexOf("H");
    expect(tIdx).toBeLessThan(sIdx);
    expect(sIdx).toBeLessThan(hIdx);
  });
});

describe('getValidPointKeys', () => {
  it('returns MKDTH (no S) for Kendo', () => {
    expect(getValidPointKeys(false)).toBe("MKDTH");
  });

  it('returns MKDTH for falsy isNaginata', () => {
    expect(getValidPointKeys(undefined)).toBe("MKDTH");
  });

  it('returns MKDTSH (with S) for Naginata', () => {
    expect(getValidPointKeys(true)).toBe("MKDTSH");
  });

  it('all button labels from getIpponButtons (including H) match a key in getValidPointKeys', () => {
    // H is a valid scoring key (Hansoku transfers a point) and IS a button —
    const kendoKeys = getValidPointKeys(false);
    getIpponButtons(false).forEach(btn => {
      expect(kendoKeys).toContain(btn);
    });
    const naginataKeys = getValidPointKeys(true);
    getIpponButtons(true).forEach(btn => {
      expect(naginataKeys).toContain(btn);
    });
  });
});

// TeamScoreEditorModal derives isNaginataTeam = !!compMeta?.config?.naginata.
// Component rendering isn't feasible (hooks are stubbed in vitest.setup.js),
// so we pin the derivation expression directly via getIpponButtons.
describe('isNaginataTeam derivation (TeamScoreEditorModal)', () => {
  it('uses Naginata button set when compMeta.config.naginata is true', () => {
    const compMeta = { config: { naginata: true } };
    expect(getIpponButtons(!!compMeta?.config?.naginata)).toEqual(["M", "K", "D", "T", "S", "H"]);
  });

  it('uses Kendo button set when compMeta.config.naginata is false', () => {
    const compMeta = { config: { naginata: false } };
    expect(getIpponButtons(!!compMeta?.config?.naginata)).toEqual(["M", "K", "D", "T", "H"]);
  });

  it('uses Kendo button set when compMeta is null (no competition loaded)', () => {
    const compMeta = null;
    expect(getIpponButtons(!!compMeta?.config?.naginata)).toEqual(["M", "K", "D", "T", "H"]);
  });

  it('uses Kendo button set when compMeta.config is missing', () => {
    const compMeta = {};
    expect(getIpponButtons(!!compMeta?.config?.naginata)).toEqual(["M", "K", "D", "T", "H"]);
  });
});

describe('decideDrawToggle (mp-42g: VS demoted, dedicated draw button)', () => {
  // decideDrawToggle is the pure predicate used by both the Mark-draw button
  // and the x/X keyboard shortcut. They now share a single toggle logic so
  // keyboard and button stay in sync.

  it('returns {action:"cancel"} when draw is already toggled (always allowed)', () => {
    expect(decideDrawToggle({ isDrawToggled: true, aTotal: 0, bTotal: 0 }))
      .toEqual({ action: "cancel" });
  });

  it('returns {action:"cancel"} even when scores exist — cancel is unconditional', () => {
    expect(decideDrawToggle({ isDrawToggled: true, aTotal: 2, bTotal: 1 }))
      .toEqual({ action: "cancel" });
  });

  it('returns {action:"enter"} when draw is not toggled and no scores exist', () => {
    expect(decideDrawToggle({ isDrawToggled: false, aTotal: 0, bTotal: 0 }))
      .toEqual({ action: "enter" });
  });

  it('returns {action:"noop"} when not toggled but aTotal > 0 (button should be disabled)', () => {
    expect(decideDrawToggle({ isDrawToggled: false, aTotal: 1, bTotal: 0 }))
      .toEqual({ action: "noop" });
  });

  it('returns {action:"noop"} when not toggled but bTotal > 0 (button should be disabled)', () => {
    expect(decideDrawToggle({ isDrawToggled: false, aTotal: 0, bTotal: 1 }))
      .toEqual({ action: "noop" });
  });

  it('returns {action:"noop"} when both sides have scored', () => {
    expect(decideDrawToggle({ isDrawToggled: false, aTotal: 1, bTotal: 2 }))
      .toEqual({ action: "noop" });
  });

  describe('keyboard and button consistency contract', () => {
    it('keyboard x with draw active → cancel (mirrors button onClick)', () => {
      const r = decideDrawToggle({ isDrawToggled: true, aTotal: 0, bTotal: 0 });
      expect(r.action).toBe("cancel");
    });

    it('keyboard x with no draw and no scores → enter (mirrors button onClick)', () => {
      const r = decideDrawToggle({ isDrawToggled: false, aTotal: 0, bTotal: 0 });
      expect(r.action).toBe("enter");
    });

    it('keyboard x with scores → noop (mirrors button disabled state)', () => {
      const r = decideDrawToggle({ isDrawToggled: false, aTotal: 1, bTotal: 0 });
      expect(r.action).toBe("noop");
    });
  });
});

describe('shouldBlockScoringKeys (hantei keyboard guard)', () => {
  // The onKeyDown handler calls shouldBlockScoringKeys(s) after the
  // isInteractiveTarget check and before any scoring-key branch. When it
  // returns true the handler exits without calling addPt or toggling draw.
  // This prevents score mutations while hantei is armed — the backend
  // requires a tied scoreline at that point (400 otherwise).

  it('returns true when decidedByHantei is true — scoring keys must be suppressed', () => {
    expect(shouldBlockScoringKeys({ decidedByHantei: true })).toBe(true);
  });

  it('returns false when decidedByHantei is false — scoring keys work normally', () => {
    expect(shouldBlockScoringKeys({ decidedByHantei: false })).toBe(false);
  });

  it('returns false when decidedByHantei is undefined (field absent)', () => {
    expect(shouldBlockScoringKeys({})).toBe(false);
  });

  it('returns false when decidedByHantei is null (defensive)', () => {
    expect(shouldBlockScoringKeys({ decidedByHantei: null })).toBe(false);
  });

  // Ordering contract: Enter and arrow keys are handled BEFORE
  // shouldBlockScoringKeys in onKeyDown (see admin_scoring_modal.jsx line ~828
  // comment "Enter and arrow keys are handled above/before this guard").
  // Because shouldBlockScoringKeys only inspects decidedByHantei — not key
  // identity — it cannot selectively suppress Enter; that invariant is
  // enforced by source ordering, not by a predicate we can unit-test here.
});
