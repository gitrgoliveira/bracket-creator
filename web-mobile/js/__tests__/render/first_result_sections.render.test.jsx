import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// bc-drev (operator decision D4 b): the first-result question is asked by the
// ONE editMatchScore the admin host hands to AdminCompetition as onEditScore.
// first_result_hosts.render.test.jsx pins what that function does; this pins
// that AdminCompetition passes it on, unchanged, to every section that records
// a result (Pools, Bracket through its inline editor, and Scores). A section
// that dropped the prop would silently lose the question on that tab.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};
const seen = { pools: null, scores: null, editor: null };

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: () => null,
  StatusBadge: Stub('StatusBadge'),
  CourtPicker: () => null,
  AdminParticipants: Stub('AdminParticipants'),
  AdminPools: (props) => { seen.pools = props; return <div data-stub="AdminPools" />; },
  AdminScoreEditor: (props) => { seen.scores = props; return <div data-stub="AdminScoreEditor" />; },
  AdminExport: Stub('AdminExport'),
  AdminTeamLineupsList: Stub('AdminTeamLineupsList'),
  ScoreEditorModal: (props) => { seen.editor = props; return <div data-testid="probe-score-editor" />; },
  BracketTree: () => null,
  MatchCard: ({ match }) => <div data-testid={`card-${match.id}`} className="bc-match" />,
  bracketColumnCount: () => 1,
  bronzeSlotLabels: () => ({ a: 'TBD', b: 'TBD' }),
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
  competitionKindLabel: () => 'Individual',
  formatDate: (d) => String(d ?? ''),
  matchMedia: () => ({ matches: false, addEventListener: noop, removeEventListener: noop, addListener: noop, removeListener: noop }),
  confirmDialog: vi.fn().mockResolvedValue(false),
  promptDialog: vi.fn().mockResolvedValue(null),
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    fetchDrawWarnings: vi.fn().mockResolvedValue([]),
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    subscribeToEvents: () => () => {},
  },
};

let restoreGlobals;
let AdminCompetition;
beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_competition.jsx');
  AdminCompetition = window.AdminCompetition;
});
afterAll(() => restoreGlobals());
beforeEach(() => {
  Element.prototype.scrollIntoView = function scrollIntoView() {};
  seen.pools = null; seen.scores = null; seen.editor = null;
});

const m1 = { id: 'm1', compId: 'c1', status: 'scheduled', court: 'A', sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } };

async function mount(section, onEditScore) {
  const comp = { id: 'c1', name: 'Mudansha', status: 'knockout', format: 'knockout', kind: 'individual', players: [], courts: ['A'], startTime: '09:00', date: '', fightingSpiritAwards: [] };
  const t = { name: 'Spring Taikai', courts: ['A'], competitions: [comp] };
  window.BracketTree = (props) => <button type="button" data-testid="pick" onClick={() => props.onMatchClick(m1, 0, 0)}>pick</button>;
  await act(async () => {
    render(
      <AdminCompetition tournament={t} competition={comp} pools={[]} poolMatches={[]} standings={[]} bracket={{ rounds: [[m1]] }}
        section={section} onSection={noop} onBack={noop} onOpenCompetition={noop} onCreateCompetition={noop}
        onUpdate={noop} onRefreshCompetition={noop} onMoveCourt={noop} onEditScore={onEditScore}
        onLogout={noop} onViewerMode={noop} tweaks={{}} password="" showToast={noop} />
    );
  });
}

describe('AdminCompetition hands onEditScore to every section that records a result (bc-drev D4 b)', () => {
  it('the Pools tab gets it', async () => {
    const onEditScore = vi.fn();
    await mount('pools', onEditScore);
    expect(seen.pools.onEditScore).toBe(onEditScore);
  });

  it('the Scores tab gets it', async () => {
    const onEditScore = vi.fn();
    await mount('scores', onEditScore);
    expect(seen.scores.onEditScore).toBe(onEditScore);
  });

  it('the Bracket tab records through it: a Start from its inline editor reaches it', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    await mount('bracket', onEditScore);
    await act(async () => { fireEvent.click(screen.getByTestId('pick')); });
    expect(seen.editor, 'the bracket panel shows the inline editor').not.toBeNull();
    const patch = { status: 'running' };
    await act(async () => { await seen.editor.onSubmit(patch, {}); });
    expect(onEditScore).toHaveBeenCalledTimes(1);
    expect(onEditScore.mock.calls[0].slice(0, 3)).toEqual(['c1', 'm1', patch]);
  });
});
