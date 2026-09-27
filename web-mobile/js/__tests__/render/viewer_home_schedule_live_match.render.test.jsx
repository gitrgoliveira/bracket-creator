// bc-dhas: a match opened from the public Home or Schedule page is read from
// the tournament's live data on every render, as on the competition page, so
// the self-run score editor it opens follows a result recorded or corrected
// on another device instead of the copy taken when it was tapped. Both pages
// list every competition's matches, and match ids repeat across
// competitions, so the match is found by its competition as well as its id.
// A match that leaves the data closes the modal for good.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const probe = { props: null };
function ProbeScoreEditor(props) {
  probe.props = props;
  return React.createElement('div', { 'data-testid': 'probe-score-editor' });
}

const STUBBED_GLOBALS = {
  ScoreEditorModal: ProbeScoreEditor,
  bracketRoundLabel: () => 'Final',
  matchScoreStr: () => '',
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null), recordScore: vi.fn() },
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ViewerHome;
let ViewerSchedule;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ ViewerHome } = await import('../../viewer_home.jsx'));
  ({ ViewerSchedule } = await import('../../viewer_schedule.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => { probe.props = null; });

const side = (id, name) => ({ id, name, dojo: '' });

// A raw bracket row as the viewer payload carries it: `round` is the engine's
// round index, which the page's label must keep overriding.
const finalRow = (overrides = {}) => ({
  id: 'k1', round: 0, status: 'running', court: 'A', scheduledAt: '10:00',
  sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
  modifiedAt: 100, ipponsA: [], ipponsB: [],
  ...overrides,
});

// Another competition whose final has the same match id, listed first, so a
// lookup by id alone would find it instead.
const otherComp = {
  id: 'c0', name: 'Veterans', kind: 'individual', teamSize: 0, format: 'knockout',
  status: 'knockout', courts: ['A'], players: [], poolMatches: [],
  bracket: { rounds: [[finalRow({ sideA: side('p8', 'Suzuki'), sideB: side('p9', 'Kato'), modifiedAt: 999, ipponsA: ['D'] })]] },
};

const openComp = (overrides = {}) => ({
  id: 'c1', name: 'Open', kind: 'individual', teamSize: 0, format: 'knockout',
  status: 'knockout', courts: ['A'], players: [], poolMatches: [],
  bracket: { rounds: [[finalRow()]] },
  ...overrides,
});

const tournament = (comp) => ({ mode: 'self-run', name: 'T', date: '', courts: ['A'], competitions: [otherComp, comp] });

const home = (t) => <ViewerHome tournament={t} onSelectCompetition={vi.fn()} onAdminClick={vi.fn()} onOpenSchedule={vi.fn()} onRegister={vi.fn()} onOpenResults={vi.fn()} />;
const schedule = (t) => <ViewerSchedule tournament={t} onBack={vi.fn()} tweaks={{ showDojo: true }} />;

// The row for the Open final: the one naming Yamada.
const yamadaRow = (selector) => [...document.querySelectorAll(selector)].find((el) => el.textContent.includes('Yamada'));

describe.each([
  ['Home', home, 'button.vsched-item'],
  ['Schedule', schedule, 'button.tw-match'],
])('%s: a match opened from the page follows the live data (bc-dhas)', (_name, page, rowSelector) => {
  async function openEditor() {
    let view;
    await act(async () => { view = render(page(tournament(openComp()))); });
    const row = yamadaRow(rowSelector);
    expect(row, 'the Open final is listed').toBeTruthy();
    await act(async () => { fireEvent.click(row); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
    return view;
  }

  it('the score editor follows its own competition\'s live row, and the page\'s fields still win', async () => {
    const view = await openEditor();
    expect(probe.props.match).toMatchObject({ id: 'k1', compId: 'c1', modifiedAt: 100, phase: 'bracket', round: 'Final', compName: 'Open' });

    await act(async () => {
      view.rerender(page(tournament(openComp({ bracket: { rounds: [[finalRow({ modifiedAt: 200, ipponsA: ['M'] })]] } }))));
    });
    expect(probe.props.match).toMatchObject({ compId: 'c1', modifiedAt: 200, ipponsA: ['M'], phase: 'bracket', round: 'Final', compName: 'Open' });
    expect(probe.props.match.sideA.name).toBe('Yamada');
  });

  it('a match that leaves the data closes the modal and does not come back with a new draw', async () => {
    const view = await openEditor();

    await act(async () => { view.rerender(page(tournament(openComp({ status: 'setup', bracket: null })))); });
    expect(screen.queryByTestId('probe-score-editor')).toBeNull();
    expect(screen.queryByRole('dialog')).toBeNull();

    await act(async () => { view.rerender(page(tournament(openComp({ bracket: { rounds: [[finalRow({ modifiedAt: 0 })]] } })))); });
    expect(screen.queryByTestId('probe-score-editor')).toBeNull();
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
