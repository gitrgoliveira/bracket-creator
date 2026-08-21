// Finding 1 (bc-dmsr second review round): preserveSubHantei (engine/scoring.go)
// distinguishes a genuinely silent writer (nil ipponsA/ipponsB - restore the
// stored verdict) from a deliberate 0-0 withdrawal (explicit [] - let it
// withdraw) purely on nil-ness. But TeamScoreEditorModal's buildPatch used to
// send explicit [] for the daihyosen row UNCONDITIONALLY, silent or not: a
// stale editor (mounted before a verdict was recorded elsewhere, then missing
// the SSE adoption - a network gap, or an offline-queue replay) saved an
// unrelated correction and erased the just-recorded verdict, because its
// "silent" write was byte-identical to a deliberate cancel.
//
// buildPatch now omits the daihyosen row's ipponsA/ipponsB entirely (so they
// decode to nil on the Go side) when BOTH: the operator never touched that
// row this session, and nothing about it is known locally either (no
// recorded verdict, no score, no fouls, no overtime). Any of those signals
// present routes through the ORIGINAL always-explicit path, so a deliberate
// withdrawal (arm -> cancel) still reaches the wire as explicit arrays.
//
// These tests mount the REAL TeamScoreEditorModal (via the ScoreEditorModal
// dispatcher, same route every production mount site uses) and drive the
// Finish button, exactly like team_editor_config_matrix.render.test.jsx and
// autosave_debounce.render.test.jsx. window.API.recordScore is a bare mock
// (not the real api_client.jsx), so the captured patch is buildPatch's own
// raw output; toBackendMatchResult (api_serializers.jsx, imported directly,
// never edited by this change) is then run over it for real to prove the
// omission survives the actual wire-serialization boundary.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { toBackendMatchResult } from '../../api_serializers.jsx';

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

beforeEach(() => {
  window.API.recordScore.mockClear();
});

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

function dhEntryOf(patch) {
  return (patch.subResults || []).find((s) => s.position === -1);
}

async function clickFinishTwice() {
  // First tap arms the button ("Finish" -> "Tap again to finish"); the
  // second commits (buildPatch("completed") -> onSubmit).
  await act(async () => { fireEvent.click(screen.getByText('Finish')); });
  await act(async () => { fireEvent.click(screen.getByText('Tap again to finish')); });
}

describe('TeamScoreEditorModal daihyosen genuine-silence discriminator', () => {
  it('omits ipponsA/ipponsB on the daihyosen row when untouched and nothing is known locally', async () => {
    // The daihyosen row EXISTS (a rep bout was requested) but carries
    // nothing: no winner, no score, no verdict. Models an editor mounted
    // before a hantei verdict was recorded on another device, whose SSE
    // adoption never landed (network gap / offline-queue replay window).
    const match = makeMatch({
      subResults: [{ position: -1, sideA: 'Kyoto', sideB: 'Osaka', decision: 'daihyosen' }],
    });

    await act(async () => {
      render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={(p) => window.API.recordScore('comp1', match.id, p, '', match)} password="" />);
    });

    // Sanity: the hantei row rendered (daihyosen exists), but the operator
    // never interacts with it in this test.
    expect(screen.getByTestId('team-daihyosen-hantei-row')).toBeTruthy();

    await clickFinishTwice();

    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    const [, , patch] = window.API.recordScore.mock.calls[0];
    const dh = dhEntryOf(patch);
    expect(dh).toBeTruthy();
    // The genuine-silence signature: the keys are ABSENT, not present-but-empty.
    expect('ipponsA' in dh).toBe(false);
    expect('ipponsB' in dh).toBe(false);
    // decidedByHantei must also be absent (not even an explicit false):
    // toBackendMatchResult only forwards ipponsA/B untouched when the flag
    // is missing entirely (its `typeof !== "boolean"` early return) - a
    // stray false here would route the row through stripHt() and turn the
    // omission right back into an explicit [].
    expect('decidedByHantei' in dh).toBe(false);

    // Prove the omission survives the REAL wire-serialization boundary too
    // (api_serializers.jsx, read-only here, never edited by this change).
    const wire = toBackendMatchResult(patch, match);
    const wireDh = wire.subResults.find((s) => s.position === -1);
    expect('ipponsA' in wireDh).toBe(false);
    expect('ipponsB' in wireDh).toBe(false);
    // And the JSON actually sent on the wire drops the keys (they decode to
    // nil on the Go side, not an empty array).
    const wireJSON = JSON.parse(JSON.stringify(wire));
    const wireJSONDh = wireJSON.subResults.find((s) => s.position === -1);
    expect(wireJSONDh.ipponsA).toBeUndefined();
    expect(wireJSONDh.ipponsB).toBeUndefined();
  });

  it('still sends explicit arrays when the operator withdraws a recorded verdict', async () => {
    // A verdict IS recorded and known locally (decidedByHantei: true, the
    // normalizeMatch-derived shape every real mount receives): Kyoto (sideA)
    // won by hantei on a 0-0 scoreline, mark riding in ipponsA.
    const match = makeMatch({
      subResults: [{
        position: -1, sideA: 'Kyoto', sideB: 'Osaka', decision: 'daihyosen',
        winner: 'Kyoto', ipponsA: ['Ht'], ipponsB: [], decidedByHantei: true,
      }],
    });

    await act(async () => {
      render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={(p) => window.API.recordScore('comp1', match.id, p, '', match)} password="" />);
    });

    // The editor adopts the recorded verdict: armed with AKA... wait, Kyoto
    // is sideA (AKA in this layout); the operator explicitly cancels it.
    expect(screen.getByTestId('team-daihyosen-hantei-row')).toBeTruthy();
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-hantei-cancel')); });

    await clickFinishTwice();

    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    const [, , patch] = window.API.recordScore.mock.calls[0];
    const dh = dhEntryOf(patch);
    expect(dh).toBeTruthy();
    // Touched (armed flipped from recorded=true to false): the row states
    // itself explicitly, decidedByHantei: false, not omitted.
    expect(dh.decidedByHantei).toBe(false);

    const wire = toBackendMatchResult(patch, match);
    const wireDh = wire.subResults.find((s) => s.position === -1);
    // Explicit, non-nil arrays reach the wire - a real withdrawal, not
    // silence. No Ht mark: the cancel un-decided the row.
    expect(Array.isArray(wireDh.ipponsA)).toBe(true);
    expect(Array.isArray(wireDh.ipponsB)).toBe(true);
    expect(wireDh.ipponsA.includes('Ht')).toBe(false);
    expect(wireDh.ipponsB.includes('Ht')).toBe(false);
    expect(wireDh.winner).toBe('');

    const wireJSON = JSON.parse(JSON.stringify(wire));
    const wireJSONDh = wireJSON.subResults.find((s) => s.position === -1);
    expect(wireJSONDh.ipponsA).toEqual([]);
    expect(wireJSONDh.ipponsB).toEqual([]);
  });
});
