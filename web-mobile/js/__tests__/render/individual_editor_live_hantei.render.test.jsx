import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// OPERATOR RULING: a scoring result must read the SAME on every surface, and
// reopening a match to edit changes only that match's result, for every
// surface asking for it.
//
// The score editor is one of those surfaces. While it is open, the viewer
// card, the bracket, the TV board, the lobby and the Excel export are all
// already showing whatever the server holds — so an editor showing something
// else is a divergence, whatever its reason.
//
// That makes a verdict recorded on ANOTHER device the interesting case. The
// editor reads its match from a live prop, so SSE delivers it mid-edit. Two
// wrong answers were tried before this one:
//
//   1. Recompute from the live prop but keep the local armed state frozen.
//      The panel still showed "Decide by hantei…", and `hanteiClear` turned
//      the live true into an authoritative `decidedByHantei: false` on the
//      next write — deleting a verdict the operator was never shown.
//   2. Freeze the flag at mount so the write goes silent. That stopped the
//      deletion by accepting the divergence: the editor sat showing no
//      verdict while every other screen showed the Ht.
//
// The answer is to ADOPT: follow the server's verdict, so there is one result
// and one reading of it. An explicit `false` from this editor is then always
// the operator ruling on something in front of them.

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

// A running 1-1 match: tied, so it is exactly the scoreline a hantei is taken
// from, and scored, so Finish is enabled.
function tiedRunningMatch(overrides = {}) {
  return {
    id: 'm1',
    status: 'running',
    phase: 'knockout',
    round: 'Semi-final',
    court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },   // AKA
    sideB: { id: 'p2', name: 'Tanaka' },   // SHIRO
    ipponsA: ['M'],
    ipponsB: ['K'],
    hansokuA: 0,
    hansokuB: 0,
    ...overrides,
  };
}

// Yamada (sideA / AKA) wins by hantei, as another device recorded it.
const withVerdict = () => tiedRunningMatch({
  decidedByHantei: true, winner: { id: 'p1', name: 'Yamada' },
});

const q = (c, sel) => c.querySelector(`[data-testid="${sel}"]`);

// Finish is a two-tap arm-then-confirm on a non-complete match. Both taps go
// through act(): doSubmit is async and lands setState after the await, which
// the render harness fails on as an unwrapped update.
async function finish(container) {
  const btn = [...container.querySelectorAll('button')]
    .find(b => /Finish|Tap again/.test(b.textContent));
  expect(btn).toBeTruthy();
  await act(async () => { fireEvent.click(btn); });
  await act(async () => { fireEvent.click(btn); });
  return btn;
}

describe('the editor shows the verdict the server holds', () => {
  it('adopts one recorded on another device, mid-edit', async () => {
    const onSubmit = vi.fn();
    const { rerender, container } = render(
      <ScoreEditorModal match={tiedRunningMatch()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    );
    // Before: no verdict anywhere, so the editor offers to record one.
    expect(q(container, 'scoring-modal-hantei-arm')).toBeTruthy();

    await act(async () => { rerender(
      <ScoreEditorModal match={withVerdict()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    ); });

    // After: the arm button is gone and the verdict row is showing, with the
    // recorded side marked — the same result the viewer and the board show.
    expect(q(container, 'scoring-modal-hantei-arm')).toBeNull();
    expect(q(container, 'scoring-modal-hantei-aka').className).toContain('btn--primary');
    expect(q(container, 'scoring-modal-hantei-shiro').className).not.toContain('btn--primary');
    // And the Ht rides in the winner's slot, as it does on every other surface.
    expect(container.textContent).toContain('Ht');
  });

  it('adopts the SCORE too, so the verdict lands in the right slot', async () => {
    // Every ippon slot is local state seeded at mount, so a stale scoreline
    // outlived an SSE update just as the verdict did — and the two together
    // were worse than either alone: adopting a verdict onto a 0-0 local board
    // put the Ht in the slot the OLD score left free, so the editor showed
    // `Ht` at 0-0 against a stored 1-1. Verified in the browser before the fix.
    const { rerender, container } = render(
      <ScoreEditorModal
        match={tiedRunningMatch({ ipponsA: [], ipponsB: [] })}
        onClose={vi.fn()} onSubmit={vi.fn()} password=""
      />
    );
    expect([...container.querySelectorAll('.sb-slot')].map(s => s.textContent))
      .toEqual(['\u00b7', '\u00b7', '\u00b7', '\u00b7']);

    await act(async () => { rerender(
      <ScoreEditorModal match={withVerdict()} onClose={vi.fn()} onSubmit={vi.fn()} password="" />
    ); });

    // SHIRO's two cells then AKA's, in DOM order. Yamada (AKA) holds the M and
    // the Ht, Tanaka (SHIRO) the K — the same result the board shows, which is
    // the whole point. (DOM order, not reading order: AKA's pair is reversed
    // visually by CSS, so on screen this reads `[K][ ] vs [Ht][M]`. Confirmed
    // against the running app, which produces exactly this array.)
    expect([...container.querySelectorAll('.sb-slot')].map(s => s.textContent))
      .toEqual(['K', '\u00b7', 'M', 'Ht']);
  });

  it('keeps UNSAVED operator edits rather than overwriting them', async () => {
    // The limit of the rule: an editor with work in it is not refreshed out
    // from under the operator. Their edits are not ours to discard, and the
    // write path reconciles (timestamp LWW server-side, plus `stale: true`).
    const { rerender, container } = render(
      <ScoreEditorModal
        match={tiedRunningMatch({ ipponsA: [], ipponsB: [] })}
        onClose={vi.fn()} onSubmit={vi.fn()} password=""
      />
    );
    // The operator awards AKA a men.
    const akaMen = [...container.querySelectorAll('button')].filter(b => b.textContent === 'M')[1];
    await act(async () => { fireEvent.click(akaMen); });
    expect([...container.querySelectorAll('.sb-slot')].map(s => s.textContent)).toContain('M');

    await act(async () => { rerender(
      <ScoreEditorModal
        match={tiedRunningMatch({ ipponsA: ['K', 'K'], ipponsB: ['D'] })}
        onClose={vi.fn()} onSubmit={vi.fn()} password=""
      />
    ); });
    const cells = [...container.querySelectorAll('.sb-slot')].map(s => s.textContent);
    expect(cells, 'the operator\u2019s unsaved men must survive').toContain('M');
    expect(cells).not.toContain('D');
  });

  it('cannot be finished past a verdict without ruling on it', async () => {
    // The adopted verdict locks scoring: Finish is disabled while it stands,
    // so no write can quietly land a different result over it.
    const onSubmit = vi.fn();
    const { rerender, container } = render(
      <ScoreEditorModal match={tiedRunningMatch()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    );
    await act(async () => { rerender(
      <ScoreEditorModal match={withVerdict()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    ); });

    const btn = await finish(container);
    expect(btn.disabled).toBe(true);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('adopting is not an unsaved change of the operators', async () => {
    // isDirty compares the verdict against what the SERVER holds, so adoption
    // moves both sides together. Closing an untouched editor must not prompt:
    // a spurious "discard unsaved scoring changes?" trains operators to
    // dismiss the one prompt that protects real work.
    const onClose = vi.fn();
    const { rerender, container } = render(
      <ScoreEditorModal match={tiedRunningMatch()} onClose={onClose} onSubmit={vi.fn()} password="" />
    );
    await act(async () => { rerender(
      <ScoreEditorModal match={withVerdict()} onClose={onClose} onSubmit={vi.fn()} password="" />
    ); });

    const cancel = [...container.querySelectorAll('button')]
      .find(b => b.textContent.trim() === 'Cancel' && !b.dataset.testid);
    await act(async () => { fireEvent.click(cancel); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('an operator who cancels the verdict on screen does clear it', async () => {
    // The other half, so adoption is not a blanket mute: the explicit false
    // still travels, because now it is always a ruling on a displayed result.
    const onSubmit = vi.fn();
    const { container } = render(
      <ScoreEditorModal match={withVerdict()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    );
    await act(async () => {
      fireEvent.click(q(container, 'scoring-modal-hantei-cancel'));
    });
    await finish(container);
    expect(onSubmit.mock.calls.at(-1)[0].decidedByHantei).toBe(false);
  });
});

// The same ruling, one level down: the TEAM editor's daihyosen verdict.
//
// It had the identical divergence, from an identical mount-frozen pair
// (initialDaihyosenHantei / initialDaihyosenHanteiArmed), and the freeze was
// what buildPatch's `hanteiKnown` guard existed to compensate for. Adopting
// removes the divergence and the guard together, so this pins the display
// half: the rep-bout panel must follow the stored verdict.
describe('the team editor shows the daihyosen verdict the server holds', () => {
  const DH = -1;
  function teamMatch(subs) {
    return {
      id: 'tm1',
      status: 'running',
      phase: 'knockout',
      round: 'Final',
      court: 'A',
      compKind: 'team',
      teamSize: 3,
      sideA: { id: 'teamA', name: 'Kyoto' },
      sideB: { id: 'teamB', name: 'Osaka' },
      subResults: subs,
    };
  }
  // A tied rep bout, first with no verdict and then decided for Kyoto.
  const bout = extra => ({
    position: DH, sideA: 'Kyoto', sideB: 'Osaka',
    ipponsA: ['M'], ipponsB: ['K'], decision: 'daihyosen', ...extra,
  });

  it('adopts a verdict recorded on another device', async () => {
    const { rerender, container } = render(
      <ScoreEditorModal match={teamMatch([bout()])} onClose={vi.fn()} onSubmit={vi.fn()} password="" />
    );
    const armed = c => /Cancel hantei|SHIRO wins|AKA wins/.test(c.textContent);
    expect(armed(container)).toBe(false);

    await act(async () => { rerender(
      <ScoreEditorModal
        match={teamMatch([bout({ decidedByHantei: true, winner: 'Kyoto' })])}
        onClose={vi.fn()} onSubmit={vi.fn()} password=""
      />
    ); });

    expect(armed(container), 'the rep-bout panel must show the stored verdict').toBe(true);
  });
});
