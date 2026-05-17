import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  resolveDecisionPassword,
  buildDecisionBody,
  shouldShowEnchoMaxBanner,
  canIncrementEncho,
  nextEnchoPeriod,
  prevEnchoPeriod,
  DecisionPrompt,
  MAX_IPPONS_PER_SIDE,
  isBoutDecided,
} from '../admin_scoring_modal.jsx';

// admin_scoring_modal.jsx ships seven module-private helpers that together
// implement the FR-033 encho-period flow (T104 cap + banner) and the
// T093–T098 decision prompt path. Each helper is exported separately so
// vitest can pin the behaviour without mounting either ScoreEditorModal /
// TeamScoreEditorModal — those run useState/useEffect at the top and the
// vitest.setup.js React mock stubs hooks, so component-level rendering
// tests would only ever see the initial render.

describe('resolveDecisionPassword', () => {
  // The /decision POST needs the operator password. The modal historically
  // didn't take one (parent did the POST), so the helper has a two-tier
  // fallback: explicit prop → window.adminPassword → "".

  let originalAdminPassword;
  beforeEach(() => {
    originalAdminPassword = window.adminPassword;
  });
  afterEach(() => {
    window.adminPassword = originalAdminPassword;
  });

  it('prefers the explicit prop when present', () => {
    window.adminPassword = 'window-password';
    expect(resolveDecisionPassword('prop-password')).toBe('prop-password');
  });

  it('falls back to window.adminPassword when prop is empty', () => {
    window.adminPassword = 'window-password';
    expect(resolveDecisionPassword('')).toBe('window-password');
    expect(resolveDecisionPassword(undefined)).toBe('window-password');
    expect(resolveDecisionPassword(null)).toBe('window-password');
  });

  it('returns "" when neither prop nor window are set', () => {
    delete window.adminPassword;
    expect(resolveDecisionPassword('')).toBe('');
    expect(resolveDecisionPassword(undefined)).toBe('');
  });

  it('treats empty-string window.adminPassword as missing (falls through to "")', () => {
    window.adminPassword = '';
    expect(resolveDecisionPassword(undefined)).toBe('');
  });
});

describe('buildDecisionBody', () => {
  // T093/T094: the /decision POST body. Pin the wire shape against the
  // server contract in handlers_decision.go: { decision, decisionBy,
  // decisionReason?, encho?: { periodCount }, force? }.

  it('builds the minimal body for kiken without reason or encho', () => {
    const body = buildDecisionBody('kiken', { decisionBy: 'shiro', decisionReason: '' }, 0);
    expect(body).toEqual({ decision: 'kiken', decisionBy: 'shiro' });
  });

  it('includes decisionReason when present', () => {
    const body = buildDecisionBody('kiken', { decisionBy: 'aka', decisionReason: 'injury' }, 0);
    expect(body).toEqual({
      decision: 'kiken',
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
    const body = buildDecisionBody('kiken', { decisionBy: 'shiro' }, 0);
    expect(body).not.toHaveProperty('encho');
  });

  it('omits encho when periodCount is negative or NaN (defensive)', () => {
    // Shouldn't happen in practice (the UI clamps at 0), but pinning
    // the > 0 check so a future refactor that uses `!= 0` doesn't
    // silently re-introduce negative counts.
    expect(buildDecisionBody('kiken', { decisionBy: 'shiro' }, -1)).not.toHaveProperty('encho');
    expect(buildDecisionBody('kiken', { decisionBy: 'shiro' }, NaN)).not.toHaveProperty('encho');
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
      const body = buildDecisionBody('kiken', { decisionBy: 'shiro' }, 0, { force: true });
      expect(body.force).toBe(true);
    });

    it('omits force when opts.force is missing or false', () => {
      expect(buildDecisionBody('kiken', { decisionBy: 'shiro' }, 0)).not.toHaveProperty('force');
      expect(buildDecisionBody('kiken', { decisionBy: 'shiro' }, 0, {})).not.toHaveProperty('force');
      expect(buildDecisionBody('kiken', { decisionBy: 'shiro' }, 0, { force: false })).not.toHaveProperty('force');
    });

    it('force combines with reason + encho cleanly', () => {
      const body = buildDecisionBody(
        'kiken',
        { decisionBy: 'aka', decisionReason: 'no-show' },
        3,
        { force: true },
      );
      expect(body).toEqual({
        decision: 'kiken',
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
  let originalAdminPassword;
  beforeEach(() => {
    originalAPI = window.API;
    originalAdminPassword = window.adminPassword;
    window.adminPassword = 'fallback-password';
    window.API = {
      recordDecision: vi.fn().mockResolvedValue({ winner: 'Tora', sideA: 'Tora', sideB: 'Kuma' }),
    };
  });
  afterEach(() => {
    window.API = originalAPI;
    window.adminPassword = originalAdminPassword;
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

  it('the parent flow: onSubmit payload + buildDecisionBody + resolveDecisionPassword → recordDecision', async () => {
    // Walk through the chain that ScoreEditorModal.submitDecision does
    // after DecisionPrompt fires. This is the surface flagged by PR #105
    // as untested: the password must reach window.API.recordDecision.
    const onSubmit = vi.fn((payload) => {
      const body = buildDecisionBody('kiken', payload, 0);
      const password = resolveDecisionPassword('explicit-pw');
      return window.API.recordDecision('comp-1', 'match-1', body, password);
    });

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
      { decision: 'kiken', decisionBy: 'aka' },
      'explicit-pw',
    );
  });

  it('parent flow falls through to window.adminPassword when no prop password', async () => {
    // Pinning the second tier of resolveDecisionPassword from the
    // caller's perspective: when ScoreEditorModal was mounted without
    // an explicit password prop, the chain still surfaces a password
    // via window.adminPassword rather than hitting /decision with "".
    const onSubmit = vi.fn((payload) => {
      const body = buildDecisionBody('fusenpai', payload, 0);
      const password = resolveDecisionPassword(''); // no explicit prop
      return window.API.recordDecision('comp-9', 'match-9', body, password);
    });
    const tree = DecisionPrompt({
      kind: 'fusenpai',
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
      'comp-9',
      'match-9',
      { decision: 'fusenpai', decisionBy: 'shiro' },
      'fallback-password',
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
      const body = buildDecisionBody('kiken', payload, 0, { force: true });
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
      { decision: 'kiken', decisionBy: 'shiro', force: true },
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
