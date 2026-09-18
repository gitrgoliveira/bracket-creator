import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, vi as viMock } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// The two ippon slots are MIRRORED on Aka: result_slot.jsx's sideSlotOrder
// returns [0, 1] for Shiro and [1, 0] for Aka, so DOM order is visual order
// and no CSS mirror is needed.
//
// That mirror is why the slot's spoken ordinal cannot be the array index.
// Labelling by index made Aka announce "slot 2" then "slot 1" while Shiro
// announced "slot 1" then "slot 2": one control, two opposite ways of
// counting, and the operator on the Aka side hears the row backwards.
//
// The array index stays the identity for key/removePt, and "slot 0 is the
// OUTER cell" stays the convention in code comments and in
// result_slot.test.jsx. This pins the USER-FACING number only: both sides
// count 1 then 2 in reading order.

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: viMock.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: viMock.fn().mockResolvedValue(null),
    recordScore: viMock.fn(),
    recordDaihyosen: viMock.fn(),
    removeDaihyosen: viMock.fn(),
    putMatchLineup: viMock.fn(),
    recordDecision: viMock.fn(),
  },
  AdminLineupHelpers: { rosterFor: viMock.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

// A running match with one ippon on each side, so some slots are filled and
// some empty: the label's ordinal must be right in both states.
function runningMatch() {
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
  };
}

// Every slot button's aria-label, in DOM order, split by the side it names.
function slotLabelsBySide(container) {
  const out = { Shiro: [], Aka: [] };
  for (const btn of container.querySelectorAll('button[aria-label]')) {
    const label = btn.getAttribute('aria-label');
    const m = /^(Shiro|Aka) slot (\d+):/.exec(label);
    if (m) out[m[1]].push(Number(m[2]));
  }
  return out;
}

describe('the ippon slots are numbered in reading order on both sides', () => {
  it('counts 1 then 2 on Shiro AND on Aka, despite the Aka mirror', () => {
    const { container } = render(
      <ScoreEditorModal match={runningMatch()} onClose={viMock.fn()} onSubmit={viMock.fn()} password="" />
    );

    const labels = slotLabelsBySide(container);

    // Both sides render their two slots.
    expect(labels.Shiro).toHaveLength(2);
    expect(labels.Aka).toHaveLength(2);

    // Reading order is 1 then 2 on BOTH. Before this fix Aka read [2, 1],
    // because the label used the mirrored ARRAY INDEX rather than the
    // position in the rendered row.
    expect(labels.Shiro).toEqual([1, 2]);
    expect(labels.Aka).toEqual([1, 2]);
  });

  it('still describes what each slot holds, so the two are distinguishable', () => {
    const { container } = render(
      <ScoreEditorModal match={runningMatch()} onClose={viMock.fn()} onSubmit={viMock.fn()} password="" />
    );

    const labels = [...container.querySelectorAll('button[aria-label]')]
      .map(b => b.getAttribute('aria-label'))
      .filter(l => / slot \d+:/.test(l));

    // The ordinal alone never identifies a slot: the contents follow it, so
    // an empty slot and a scored one never read the same.
    expect(labels.some(l => /:\s*remove /.test(l))).toBe(true);
    expect(labels.some(l => /:\s*empty$/.test(l))).toBe(true);
  });
});
