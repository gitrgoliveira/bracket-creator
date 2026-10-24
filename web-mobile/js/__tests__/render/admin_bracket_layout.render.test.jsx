import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// The admin Bracket page (operator decision): the bracket takes the full width
// and its columns narrow to fit (.bracket-canvas--fit), the scoring panel sits
// BELOW it, and picking a match scrolls that panel into view. The 3rd-place
// card sits in a row of column slots sized like the tree's columns, the last
// one under the final.

const STUBBED_GLOBALS = {
  ScoreEditorModal: () => <div data-testid="probe-score-editor" />,
  BracketTree: () => null, // per-test override drives the pick
  MatchCard: ({ match }) => <div data-testid={`card-${match.id}`} className="bc-match" />,
  bracketColumnCount: () => 3,
  bronzeSlotLabels: () => ({ a: 'TBD', b: 'TBD' }),
  CourtPicker: () => null,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    subscribeToEvents: () => () => {},
  },
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let AdminBracket;
let scrolled;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_competition_bracket.jsx');
  AdminBracket = window.AdminBracket;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  scrolled = [];
  // jsdom has no scrollIntoView: record which element asked to be shown.
  Element.prototype.scrollIntoView = function scrollIntoView() { scrolled.push(this); };
});

const match = (id) => ({
  id, compId: 'c1', status: 'scheduled', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Suzuki' },
});

// BracketTree probe: expose the tree's onMatchClick as a button.
const pickable = (m) => (props) => (
  <button data-testid="pick" onClick={() => props.onMatchClick(m, 0, 0)}>pick</button>
);

const renderPage = (bracket) => render(
  <AdminBracket
    c={{ id: 'c1', name: 'Comp', engi: false }}
    t={{ courts: ['A'] }}
    bracket={bracket}
    onMoveCourt={vi.fn()}
    onEditScore={vi.fn()}
    tweaks={{}}
    password="pw"
  />
);

describe('admin Bracket page layout', () => {
  it('narrows the tree to fit and scrolls the panel below it into view when a match is picked', async () => {
    const m = match('m1');
    window.BracketTree = pickable(m);
    let utils;
    await act(async () => { utils = renderPage({ rounds: [[m]] }); });
    expect(utils.container.querySelector('.bracket-canvas').classList.contains('bracket-canvas--fit')).toBe(true);
    expect(scrolled).toHaveLength(0);

    await act(async () => { fireEvent.click(screen.getByTestId('pick')); });
    expect(scrolled).toHaveLength(1);
    expect(scrolled[0].classList.contains('bracket-layout__panel')).toBe(true);

    // A later update of the same pick (an SSE refresh) does not scroll again.
    await act(async () => {
      utils.rerender(
        <AdminBracket c={{ id: 'c1', name: 'Comp', engi: false }} t={{ courts: ['A'] }}
          bracket={{ rounds: [[{ ...m, status: 'running' }]] }}
          onMoveCourt={vi.fn()} onEditScore={vi.fn()} tweaks={{}} password="pw" />
      );
    });
    expect(scrolled).toHaveLength(1);
  });

  it('puts the 3rd-place card in the last of the column slots, under the final', async () => {
    window.BracketTree = () => null;
    let utils;
    await act(async () => {
      utils = renderPage({ rounds: [[match('q1'), match('q2')], [match('s1')], [match('f1')]], thirdPlaceMatch: match('b1') });
    });
    const slots = utils.container.querySelectorAll('.bracket-bronze-row > .bracket-bronze-row__slot');
    expect(slots).toHaveLength(3);
    expect(slots[0].children).toHaveLength(0);
    expect(slots[1].children).toHaveLength(0);
    expect(slots[2].querySelector('[data-testid="card-b1"]')).toBeTruthy();
    expect(slots[2].textContent).toContain('3rd Place Match');
  });
});

// Operator decision R1 (stage 5 review 8): choosing another match puts it in
// the editor's place (keyed by match id), so over the editor's unsaved
// entries the panel asks first. Keep entries keeps the editor on its match.
describe('the bracket panel asks before it drops the editor\'s unsaved entries (R1)', () => {
  it('asks over unsaved entries, and only then', async () => {
    const m1 = match('m1');
    const m2 = { ...match('m2'), sideA: { id: 'p3', name: 'Kato' }, sideB: { id: 'p4', name: 'Ito' } };
    const shown = { props: null };
    const prevEditor = window.ScoreEditorModal;
    const prevConfirm = window.confirmDialog;
    window.ScoreEditorModal = (props) => { shown.props = props; return <div data-testid="probe-score-editor" data-match={props.match.id} />; };
    window.BracketTree = (props) => (
      <>
        <button data-testid="pick1" onClick={() => props.onMatchClick(m1, 0, 0)}>1</button>
        <button data-testid="pick2" onClick={() => props.onMatchClick(m2, 0, 1)}>2</button>
      </>
    );
    try {
      await act(async () => { renderPage({ rounds: [[m1, m2]] }); });
      await act(async () => { fireEvent.click(screen.getByTestId('pick1')); });
      expect(screen.getByTestId('probe-score-editor').getAttribute('data-match')).toBe('m1');
      shown.props.hostUnsavedRef.current = () => true;
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await act(async () => { fireEvent.click(screen.getByTestId('pick2')); });
      expect(window.confirmDialog.mock.calls.map(([o]) => o.message)).toEqual(['Your unsaved entries will be discarded. Leave this match anyway?']);
      expect(screen.getByTestId('probe-score-editor').getAttribute('data-match')).toBe('m1');
      window.confirmDialog = vi.fn().mockResolvedValue(true);
      await act(async () => { fireEvent.click(screen.getByTestId('pick2')); });
      expect(screen.getByTestId('probe-score-editor').getAttribute('data-match')).toBe('m2');
      shown.props.hostUnsavedRef.current = () => false;
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await act(async () => { fireEvent.click(screen.getByTestId('pick1')); });
      expect(window.confirmDialog).not.toHaveBeenCalled();
      expect(screen.getByTestId('probe-score-editor').getAttribute('data-match')).toBe('m1');
    } finally {
      window.ScoreEditorModal = prevEditor;
      window.confirmDialog = prevConfirm;
    }
  });
});
