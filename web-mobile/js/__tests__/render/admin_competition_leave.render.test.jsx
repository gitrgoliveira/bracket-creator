import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// Operator decision R1 (2026-10-17), stage 5 review 9 M1: every way off the
// competition page while the Bracket section's inline score editor holds
// unsaved entries asks first ("Your unsaved entries will be discarded. Leave
// this match anyway?"): the section tabs, the breadcrumb, another competition,
// + Add competition, Sign out, Public viewer and the running strip. Keep
// entries stays; Discard entries goes. With nothing unsaved each goes in the
// same tick and nothing asks.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};
const chrome = { topbar: null, crumbs: null };
const editor = { props: null };

const STUBBED_GLOBALS = {
  AdminTopbar: (props) => { chrome.topbar = props; return <div data-stub="AdminTopbar" />; },
  Breadcrumbs: (props) => { chrome.crumbs = props; return null; },
  StatusBadge: Stub('StatusBadge'),
  CourtPicker: () => null,
  AdminParticipants: Stub('AdminParticipants'),
  AdminPools: Stub('AdminPools'),
  AdminScoreEditor: Stub('AdminScoreEditor'),
  AdminExport: Stub('AdminExport'),
  AdminTeamLineupsList: Stub('AdminTeamLineupsList'),
  ScoreEditorModal: (props) => { editor.props = props; return <div data-testid="probe-score-editor" data-match={props.match.id} />; },
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
    discardDraw: vi.fn().mockResolvedValue({}),
    startCompetition: vi.fn().mockResolvedValue({}),
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
  editor.props = null;
});

const WARN = /^Your unsaved entries will be discarded\. /;
const m1 = { id: 'm1', compId: 'c1', status: 'running', court: 'A', sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } };

async function mountBracket(status = 'knockout') {
  const comp = { id: 'c1', name: 'Mudansha', status, format: 'knockout', kind: 'individual', players: [], courts: ['A'], startTime: '09:00', date: '', fightingSpiritAwards: [] };
  const t = { name: 'Spring Taikai', courts: ['A'], competitions: [comp, { id: 'c2', name: 'Yudansha' }] };
  const exits = { onSection: vi.fn(), onBack: vi.fn(), onOpenCompetition: vi.fn(), onCreateCompetition: vi.fn(), onLogout: vi.fn(), onViewerMode: vi.fn() };
  window.BracketTree = (props) => <button type="button" data-testid="pick" onClick={() => props.onMatchClick(m1, 0, 0)}>pick</button>;
  let utils;
  await act(async () => {
    utils = render(
      <AdminCompetition tournament={t} competition={comp} pools={[]} poolMatches={[]} standings={[]} bracket={{ rounds: [[m1]] }}
        section="bracket" onUpdate={noop} onRefreshCompetition={noop} onMoveCourt={noop} onEditScore={noop}
        tweaks={{}} password="" showToast={noop} {...exits} />
    );
  });
  await act(async () => { fireEvent.click(screen.getByTestId('pick')); });
  expect(editor.props, 'the bracket panel shows the inline editor').not.toBeNull();
  return { utils, exits };
}

const navButton = (c, label) => [...c.querySelectorAll('.side-nav button')].find((b) => b.textContent.trim() === label);

describe('the competition page asks before it drops the bracket editor\'s unsaved entries (R1)', () => {
  it('every exit asks over unsaved entries; Keep entries stays, Discard entries goes, nothing unsaved goes at once', async () => {
    const { utils, exits } = await mountBracket();
    const c = utils.container;
    const cases = [
      ['a section tab', 'onSection', () => navButton(c, 'Scores').click()],
      ['the competition crumb', 'onSection', () => chrome.crumbs.items[1].onClick()],
      ['the Dashboard crumb', 'onBack', () => chrome.crumbs.items[0].onClick()],
      ['another competition', 'onOpenCompetition', () => navButton(c, 'Yudansha').click()],
      ['+ Add competition', 'onCreateCompetition', () => navButton(c, '+ Add competition').click()],
      ['Sign out', 'onLogout', () => chrome.topbar.onLogout()],
      ['Public viewer', 'onViewerMode', () => chrome.topbar.onViewerMode()],
    ];
    const failures = [];
    for (const [label, fn, take] of cases) {
      const before = exits[fn].mock.calls.length;
      editor.props.hostUnsavedRef.current = () => true;
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await act(async () => { take(); });
      const asked = window.confirmDialog.mock.calls.filter(([o]) => WARN.test((o && o.message) || '')).map(([o]) => o.message);
      if (asked.length !== 1 || asked[0] !== 'Your unsaved entries will be discarded. Leave this match anyway?') failures.push(`${label}: asked ${JSON.stringify(asked)}`);
      if (exits[fn].mock.calls.length !== before) failures.push(`${label}: Keep entries left anyway`);
      window.confirmDialog = vi.fn().mockResolvedValue(true);
      await act(async () => { take(); });
      if (exits[fn].mock.calls.length !== before + 1) failures.push(`${label}: Discard entries did not leave`);
      editor.props.hostUnsavedRef.current = () => false;
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      take();
      if (exits[fn].mock.calls.length !== before + 2) failures.push(`${label}: nothing unsaved did not leave in the same tick`);
      if (window.confirmDialog.mock.calls.length) failures.push(`${label}: asked with nothing unsaved`);
    }
    expect(failures).toEqual([]);
  });

  it('the running strip takes the same question (the topbar is handed it)', async () => {
    await mountBracket();
    expect(typeof chrome.topbar.beforeLeave).toBe('function');
    editor.props.hostUnsavedRef.current = () => true;
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    let ok;
    await act(async () => { ok = await chrome.topbar.beforeLeave(); });
    expect(ok).toBe(false);
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    editor.props.hostUnsavedRef.current = () => false;
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    await act(async () => { ok = await chrome.topbar.beforeLeave(); });
    expect(ok).toBe(true);
    expect(window.confirmDialog).not.toHaveBeenCalled();
  });
});
