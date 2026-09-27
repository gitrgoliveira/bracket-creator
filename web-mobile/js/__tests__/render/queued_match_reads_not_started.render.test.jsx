// bc-sbq (operator ruling 2026-09-26): a match sent back to the queue KEEPS
// its score on the server (points, penalties, overtime, team bouts), but while
// it waits in the queue every screen shows it as NOT STARTED: the pairing, a
// plain "vs", empty slots, no penalty triangle, no IV/PW. The kept score
// reappears once the match runs again. matchShowsScore (match_shows_score.jsx) is the
// one predicate; the shared scoreboard components gate themselves on it, so
// every host inherits the rule. Each describe below pins one host: the queued
// case hides the kept score, and the SAME match running shows it, so the
// hiding is attributable to the status and not to a fixture that never
// carried a score.
//
// bracket.jsx is imported first: it publishes the window.* globals
// (matchMiddleMark, matchScoreStr, boutMiddle, ...) the scoreboard and the
// viewer rows reach bracket.jsx through. matchShowsScore itself is a leaf
// every host imports directly.
import React from 'react';
import { render, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, afterEach, vi } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let IndividualScore, TeamScoreboard, MatchDetailCard, VSchedItem, MatchCard, LobbyMatchCell, TvDisplay;

beforeAll(async () => {
  await import('../../bracket.jsx');
  MatchCard = window.MatchCard;
  ({ IndividualScore, TeamScoreboard } = await import('../../match_scoreboard.jsx'));
  ({ MatchDetailCard, VSchedItem } = await import('../../viewer_match.jsx'));
  ({ LobbyMatchCell } = await import('../../display_lobby.jsx'));
  ({ TvDisplay } = await import('../../display.jsx'));
});

afterEach(() => cleanup());

const aka = { id: 'p-aka', name: 'Aoki Taro' };
const shiro = { id: 'p-shiro', name: 'Endo Goro' };

// An individual match holding a kept Men for Aka, one outstanding hansoku for
// Shiro, and an overtime: everything a queued match can carry.
const individual = (status) => ({
  id: 'Pool A-0', compId: 'c1', court: 'A', phase: 'bracket', round: 'QF', status,
  sideA: aka, sideB: shiro,
  ipponsA: ['M'], ipponsB: [], hansokuA: 0, hansokuB: 1,
  encho: { periodCount: 1 },
});

// Team bouts already fought: Shiro's fighter won bout 1 with a Men.
const teamSubs = () => [
  { position: 1, sideA: 'Aka One', sideB: 'Shiro One', winner: 'Shiro One', ipponsA: [], ipponsB: ['M'] },
];
const team = (status) => ({
  id: 'Pool A-0', compId: 'c1', court: 'A', phase: 'bracket', round: 'QF', status,
  compKind: 'team', teamSize: 3,
  sideA: { id: 't-aka', name: 'Red Team' }, sideB: { id: 't-shiro', name: 'White Team' },
  subResults: teamSubs(),
  teamResult: { shiroIV: 1, akaIV: 0, shiroPW: 1, akaPW: 0 },
});

const slotText = (container) =>
  [...container.querySelectorAll('.msb-slot')].map((s) => s.textContent).join('');

describe('IndividualScore: a queued match reads not started', () => {
  it('shows no kept point, no penalty mark and a plain "vs" while scheduled', () => {
    const { container } = render(<IndividualScore match={individual('scheduled')} showNames />);
    expect(slotText(container)).not.toContain('M');
    expect(container.querySelector('.msb-hansoku')).toBeNull();
    expect(container.querySelector('.msb-vs').textContent).toBe('vs');
    // The pairing itself still shows.
    expect(container.textContent).toContain('Aoki Taro');
    expect(container.textContent).toContain('Endo Goro');
  });

  it('shows the kept score again once the match runs', () => {
    const { container } = render(<IndividualScore match={individual('running')} showNames />);
    expect(slotText(container)).toContain('M');
    expect(container.querySelector('[data-testid="foul-mark-b"]').textContent).toBe('▲');
    expect(container.querySelector('.msb-vs').textContent).toBe('(E)');
  });
});

describe('TeamScoreboard: a queued team match reads not started', () => {
  const props = (status) => ({
    subResults: teamSubs(), teamResult: { shiroIV: 1, akaIV: 0, shiroPW: 1, akaPW: 0 },
    teamSize: 3, showDH: true, variant: 'card', status,
    shiroName: 'White Team', akaName: 'Red Team', matchSideA: 'Red Team', matchSideB: 'White Team',
  });
  const summaryFigures = (container) =>
    [...container.querySelectorAll('[data-testid="team-summary"] .msb-sum')].map((s) => s.textContent);

  it('shows no fought bout, IV/PW 0 and no Daihyosen row while scheduled', () => {
    const { container } = render(<TeamScoreboard {...props('scheduled')} />);
    expect(slotText(container).replace(/IV|PW|\d/g, '')).not.toContain('M');
    expect(summaryFigures(container)).toEqual(['IV0', 'PW0', 'PW0', 'IV0']);
    // showDH arrives true from a host that read the REAL bouts; the blanked
    // aggregate reads tied, so without the gate this would say "pending".
    expect(container.textContent).not.toContain('Daihyosen pending');
  });

  it('shows the fought bout and the aggregate once the match runs', () => {
    const { container } = render(<TeamScoreboard {...props('running')} />);
    expect(slotText(container)).toContain('M');
    expect(summaryFigures(container)).toEqual(['IV1', 'PW1', 'PW0', 'IV0']);
  });
});

describe('public match card (MatchDetailCard): a queued match reads not started', () => {
  it('individual: no kept point and no penalty mark while scheduled', () => {
    const { container } = render(<MatchDetailCard match={individual('scheduled')} escapeToClose={false} />);
    expect(slotText(container)).not.toContain('M');
    expect(container.querySelector('.msb-hansoku')).toBeNull();
    expect(container.querySelector('.msb-vs').textContent).toBe('vs');
  });

  it('individual: the kept point and penalty mark return once running', () => {
    const { container } = render(<MatchDetailCard match={individual('running')} escapeToClose={false} />);
    expect(slotText(container)).toContain('M');
    expect(container.querySelector('[data-testid="foul-mark-b"]')).not.toBeNull();
  });

  it('team: no fought bout and IV/PW 0 while scheduled, both back once running', () => {
    const queued = render(<MatchDetailCard match={team('scheduled')} escapeToClose={false} />);
    expect(slotText(queued.container).replace(/IV|PW|\d/g, '')).not.toContain('M');
    expect(queued.container.querySelector('[data-testid="team-summary"]').textContent).not.toContain('IV1');
    cleanup();
    const running = render(<MatchDetailCard match={team('running')} escapeToClose={false} />);
    expect(slotText(running.container)).toContain('M');
    expect(running.container.querySelector('[data-testid="team-summary"]').textContent).toContain('IV1');
  });
});

describe('TV board: an up-next match that kept its score reads not started', () => {
  const comp = (matches, extra = {}) => ({
    id: 'c1', name: 'Cup', format: 'mixed', withZekkenName: false,
    poolMatches: matches, bracket: { rounds: [] }, ...extra,
  });

  it('individual pool board: the promoted up-next row shows no kept point or penalty', () => {
    const m = { ...individual('scheduled'), id: 'Pool A-0' };
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[comp([m], { kind: 'individual', teamSize: 0 })]} />
    );
    expect(container.querySelector('[data-testid="individual-score"]')).not.toBeNull();
    expect(slotText(container)).not.toContain('M');
    expect(container.querySelector('.msb-hansoku')).toBeNull();
  });

  it('team board: the headline IV/PW of an up-next match is 0/0, and the running one shows its own', () => {
    const teamComp = (status) => comp([{ ...team(status), id: 'Pool A-0' }], { kind: 'team', teamSize: 3 });
    const queued = render(<TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[teamComp('scheduled')]} />);
    expect(queued.container.querySelector('[data-testid="headline-ivpw-shiro"]').textContent).toBe('IV 0PW 0');
    expect(slotText(queued.container)).not.toContain('M');
    cleanup();
    const running = render(<TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[teamComp('running')]} />);
    expect(running.container.querySelector('[data-testid="headline-ivpw-shiro"]').textContent).toBe('IV 1PW 1');
  });
});

describe('lobby board: a queued team encounter reads "vs"', () => {
  const cell = (status) => render(
    <table><tbody><tr>
      <LobbyMatchCell rowKind="next" slot={{ match: team(status), competition: { id: 'c1', name: 'Cup', kind: 'team', teamSize: 3 } }} />
    </tr></tbody></table>
  );

  it('shows the plain "vs" while scheduled and the IV/PW once running', () => {
    expect(cell('scheduled').container.querySelector('[data-testid="lobby-team-centre"]').textContent).toBe('vs');
    cleanup();
    expect(cell('running').container.querySelector('[data-testid="lobby-team-centre"]').textContent).toContain('IV');
  });
});

describe('bracket card: a queued match carries no (E) chip', () => {
  it('hides the kept overtime while scheduled and shows it once running', () => {
    const queued = render(<MatchCard match={individual('scheduled')} variant="1" />);
    expect(queued.container.querySelector('.bc-encho')).toBeNull();
    cleanup();
    const running = render(<MatchCard match={individual('running')} variant="1" />);
    expect(running.container.querySelector('.bc-encho')).not.toBeNull();
  });
});

describe('public schedule row (VSchedItem): a queued match reads "vs"', () => {
  it('shows the plain "vs" for a scheduled match that kept an overtime', () => {
    const { container } = render(<VSchedItem m={individual('scheduled')} tweaks={{}} />);
    expect(container.querySelector('.vsched-item__vs').textContent).toBe('vs');
    expect(container.textContent).not.toContain('(E)');
  });
});

describe('admin Scores tab: a queued match shows no foul mark', () => {
  const queued = { ...individual('scheduled'), id: 'Pool A-0', scheduledAt: '10:00' };
  const running = { ...individual('running'), id: 'Pool A-1', scheduledAt: '10:10',
    sideA: { id: 'p-a2', name: 'Sato Ken' }, sideB: { id: 'p-b2', name: 'Ito Rei' } };
  const STUBS = {
    ScoreEditorModal: () => <div data-testid="score-editor" />,
    AdminTopbar: ({ children }) => <div>{children}</div>,
    Breadcrumbs: () => null,
    CourtPicker: () => null,
    getScoreBtnClass: () => 'test-score-open',
    filterMatchesByCourt: (matches) => matches,
    tournamentMatches: () => [],
    compMatches: () => [queued, running],
    confirmDialog: vi.fn().mockResolvedValue(true),
    pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || s + 's')}`,
    API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null) },
  };
  let restore, AdminScoreEditor;
  beforeAll(async () => {
    window.scrollTo = vi.fn();
    restore = installWindowStubs(STUBS);
    ({ AdminScoreEditor } = await import('../../admin_schedule_score_editor.jsx'));
  });
  afterAll(() => restore());

  it('marks the running row\'s penalty and not the queued row\'s', () => {
    const { container } = render(
      <AdminScoreEditor t={{ competitions: [{ id: 'c1', name: 'Cup' }] }} onEditScore={vi.fn()} onMoveCourt={null} password="pw" showToast={vi.fn()} />
    );
    const rows = [...container.querySelectorAll('.score-edit-row')];
    const queuedRow = rows.find((r) => r.textContent.includes('Aoki Taro'));
    const runningRow = rows.find((r) => r.textContent.includes('Sato Ken'));
    expect(queuedRow).toBeTruthy();
    expect(runningRow).toBeTruthy();
    expect(queuedRow.querySelector('[data-testid="foul-mark-b"]')).toBeNull();
    expect(queuedRow.querySelector('.score-edit-row__scoreval').textContent).toBe('vs');
    expect(runningRow.querySelector('[data-testid="foul-mark-b"]').textContent).toBe('▲');
  });
});
