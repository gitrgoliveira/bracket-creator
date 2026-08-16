import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// A second device recording a hantei must not make an ALREADY-OPEN individual
// score editor erase it.
//
// The editor reads its match from a live prop: SSE updates flow straight in
// while the operator is mid-edit. `initialDecidedByHantei` used to be a plain
// per-render const off that prop, so a verdict recorded elsewhere flipped it
// true — and `hanteiClear` turns a true into an authoritative
// `decidedByHantei: false` on the next write. The result was that the operator
// who had NOT been shown the verdict silently deleted it, plus a spurious
// "discard unsaved changes?" prompt on an untouched editor (isDirty compares
// the live value against frozen local state).
//
// It is the same failure the team editor's frozen initialDaihyosenHanteiArmed +
// hanteiKnown guard was added to prevent, one level up. Frozen at mount, an
// editor that never saw the verdict says NOTHING about it, and the server's
// preserveMatchHantei keeps it.
//
// This became consequential in this PR: before a pool hantei persisted
// (state.encodeHanteiIntoIppons) there was nothing on disk for a pool match to
// lose.

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
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ipponsA: ['M'],
    ipponsB: ['K'],
    hansokuA: 0,
    hansokuB: 0,
    ...overrides,
  };
}

// Finish is a two-tap arm-then-confirm on a non-complete match. Both taps go
// through act(): doSubmit is async and lands setState after the await, which
// the render harness fails on as an unwrapped update.
async function finish(container) {
  const btn = [...container.querySelectorAll('button')]
    .find(b => /Finish|Tap again/.test(b.textContent));
  expect(btn).toBeTruthy();
  await act(async () => { fireEvent.click(btn); });
  await act(async () => { fireEvent.click(btn); });
}

describe('an SSE-delivered hantei does not make an open editor erase it', () => {
  it('a write from an editor mounted BEFORE the verdict stays silent', async () => {
    const onSubmit = vi.fn();
    const { rerender, container } = render(
      <ScoreEditorModal match={tiedRunningMatch()} onClose={vi.fn()} onSubmit={onSubmit} password="" />
    );

    // Another device records Yamada the winner by hantei; SSE updates the prop.
    await act(async () => { rerender(
      <ScoreEditorModal
        match={tiedRunningMatch({ decidedByHantei: true, winner: { id: 'p1', name: 'Yamada' } })}
        onClose={vi.fn()} onSubmit={onSubmit} password=""
      />
    ); });

    await finish(container);

    expect(onSubmit).toHaveBeenCalled();
    const patch = onSubmit.mock.calls.at(-1)[0];
    expect(patch, 'this editor was never shown the verdict, so it must not rule on it')
      .not.toHaveProperty('decidedByHantei');
  });

  it('an editor mounted WITH the verdict still clears it on a normal finish', async () => {
    // The other half, so the fix is a narrowing and not a blanket mute: an
    // operator who can see the Ht is re-scoring the match by the ordinary flow,
    // and that removes the marker (the pre-existing contract).
    const onSubmit = vi.fn();
    const { container } = render(
      <ScoreEditorModal
        match={tiedRunningMatch({ decidedByHantei: true, winner: { id: 'p1', name: 'Yamada' } })}
        onClose={vi.fn()} onSubmit={onSubmit} password=""
      />
    );

    // Arming locks the score buttons, so cancel the recorded verdict first —
    // the operator's explicit "no, re-score this" action.
    await act(async () => {
      fireEvent.click(container.querySelector('[data-testid="scoring-modal-hantei-cancel"]'));
    });
    await finish(container);

    const patch = onSubmit.mock.calls.at(-1)[0];
    expect(patch.decidedByHantei).toBe(false);
  });
});
