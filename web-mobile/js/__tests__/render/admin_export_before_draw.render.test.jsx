// The Export & print tab before the draw and around the first result:
// - before the draw it says only a blank template can be downloaded until the
//   draw is generated, and what sizes it;
// - a template the server cannot size says its sentence on the tab, beside the
//   button, until the next try;
// - it warns while the competitor list was never shuffled and a shuffle could
//   change anything (at least two unseeded), with the remedy for setup and for
//   a drawn competition, never once a result exists or for Swiss;
// - Download results appears with the first recorded result a push brings,
//   and goes again when that result is cleared.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../../api_client.jsx';
import {
  AdminExport, PRE_DRAW_NOTICE, NOT_SHUFFLED_BEFORE_DRAW, NOT_SHUFFLED_DRAWN, notShuffledWarning,
} from '../../admin_schedule_export.jsx';
import { canShuffleUnseeded } from '../../competition_shape.jsx';
import { normalizeCompetitionDetail } from '../../api_serializers.jsx';
import { applyPatch } from '../../patch.jsx';

const players = (seeds) => seeds.map((seed, i) => ({ id: `p${i}`, name: `P${i}`, dojo: `D${i}`, seed }));

const competition = ({ status = 'setup', format = 'pools', rosterShuffled, seeds = [null, null, null] } = {}) => ({
  id: 'c1',
  name: 'Open Men',
  status,
  config: {
    format,
    poolSize: 3,
    poolWinners: 2,
    courts: ['A'],
    rosterShuffled,
    players: players(seeds),
  },
});

const renderTab = (c, props = {}) => render(<AdminExport c={c} t={{ id: 't1' }} password="pw" {...props} />);
const byTestId = (container, id) => container.querySelector(`[data-testid="${id}"]`);
const templateButton = (container) => Array.from(container.querySelectorAll('button')).find(
  (b) => b.textContent.includes('Download blank template') || b.textContent.includes('Preparing'),
);
const labels = (container) => Array.from(container.querySelectorAll('button')).map((b) => b.textContent);

describe('AdminExport pre-draw notice', () => {
  it('says only a blank template exists until the draw, and what sizes it', () => {
    const { container } = renderTab(competition());
    const notice = byTestId(container, 'export-pre-draw-notice');
    expect(notice.textContent).toBe(PRE_DRAW_NOTICE);
    expect(PRE_DRAW_NOTICE).toMatch(/until the draw is generated, only a blank template can be downloaded/);
    expect(PRE_DRAW_NOTICE).toMatch(/the larger of the expected number of participants \(set in Settings\) and everyone registered/);
  });

  it('shows for a legacy competition with no status, which has no draw either', () => {
    const c = competition();
    c.status = undefined;
    expect(byTestId(renderTab(c).container, 'export-pre-draw-notice')).not.toBeNull();
  });

  it('is gone once a draw exists, and never shows for Swiss', () => {
    expect(byTestId(renderTab(competition({ status: 'draw-ready' })).container, 'export-pre-draw-notice')).toBeNull();
    expect(byTestId(renderTab(competition({ format: 'swiss' })).container, 'export-pre-draw-notice')).toBeNull();
  });
});

describe('AdminExport refusal sentence', () => {
  const originalFetch = global.fetch;
  const originalAPI = window.API;
  const originalCreate = window.URL.createObjectURL;
  const originalRevoke = window.URL.revokeObjectURL;
  beforeEach(() => {
    window.API = API;
    window.URL.createObjectURL = vi.fn(() => 'blob:template');
    window.URL.revokeObjectURL = vi.fn();
    vi.spyOn(window, 'alert').mockImplementation(() => {});
  });
  afterEach(() => {
    global.fetch = originalFetch;
    window.API = originalAPI;
    window.URL.createObjectURL = originalCreate;
    window.URL.revokeObjectURL = originalRevoke;
    vi.restoreAllMocks();
  });

  const SENTENCE = "no draw yet, and fewer than 2 places to print: set the expected number of participants in this competition's Settings, or add participants";
  const refusal = () => Promise.resolve({
    ok: false,
    status: 422,
    headers: new Headers(),
    json: () => Promise.resolve({ error: SENTENCE }),
  });
  const xlsx = () => Promise.resolve({
    ok: true,
    status: 200,
    headers: new Headers(),
    blob: () => Promise.resolve(new Blob(['xlsx'])),
    json: () => Promise.resolve({}),
  });

  it('says the server sentence on the tab, beside the button, and no alert', async () => {
    global.fetch = vi.fn(refusal);
    const { container } = renderTab(competition({ seeds: [] }));
    await act(async () => { fireEvent.click(templateButton(container)); });
    const error = byTestId(container, 'export-template-error');
    expect(error.textContent).toBe(`Export failed: ${SENTENCE}`);
    expect(error.getAttribute('role')).toBe('alert');
    expect(window.alert).not.toHaveBeenCalled();
  });

  it('takes the sentence down when the next try downloads', async () => {
    global.fetch = vi.fn(refusal);
    const { container } = renderTab(competition({ seeds: [] }));
    await act(async () => { fireEvent.click(templateButton(container)); });
    expect(byTestId(container, 'export-template-error')).not.toBeNull();
    global.fetch = vi.fn(xlsx);
    await act(async () => { fireEvent.click(templateButton(container)); });
    expect(byTestId(container, 'export-template-error')).toBeNull();
    expect(window.URL.createObjectURL).toHaveBeenCalledTimes(1);
  });
});

describe('AdminExport never-shuffled warning', () => {
  const warning = (opts) => byTestId(renderTab(competition(opts)).container, 'export-not-shuffled');

  it('warns in setup, naming the cost and the fix', () => {
    expect(warning({}).textContent).toBe(NOT_SHUFFLED_BEFORE_DRAW);
    expect(NOT_SHUFFLED_BEFORE_DRAW).toMatch(/order of the list/);
    expect(NOT_SHUFFLED_BEFORE_DRAW).toMatch(/Shuffle unseeded on Participants/);
  });

  it('warns on a drawn competition with the discard, shuffle, generate remedy', () => {
    expect(warning({ status: 'draw-ready' }).textContent).toBe(NOT_SHUFFLED_DRAWN);
    expect(NOT_SHUFFLED_DRAWN).toMatch(/discard the draw, tap Shuffle unseeded on Participants, then generate the draw again/);
    expect(NOT_SHUFFLED_DRAWN).toMatch(/not recorded as shuffled/);
  });

  it('is gone once the list was shuffled', () => {
    expect(warning({ rosterShuffled: true })).toBeNull();
    expect(warning({ status: 'draw-ready', rosterShuffled: true })).toBeNull();
  });

  it('reads the marker from the top level too (a flattened competition)', () => {
    const c = competition();
    c.rosterShuffled = true;
    expect(byTestId(renderTab(c).container, 'export-not-shuffled')).toBeNull();
  });

  it('says nothing when a shuffle could change nothing: fewer than two unseeded', () => {
    expect(warning({ seeds: [] })).toBeNull();
    expect(warning({ seeds: [null] })).toBeNull();
    expect(warning({ seeds: [1, 2, null] })).toBeNull();
    expect(warning({ seeds: [1, null, null] }).textContent).toBe(NOT_SHUFFLED_BEFORE_DRAW);
  });

  it('says nothing once the competition is under way, or for Swiss', () => {
    for (const status of ['pools', 'knockout', 'completed']) {
      expect(warning({ status })).toBeNull();
    }
    expect(warning({ format: 'swiss' })).toBeNull();
  });

  it('asks the same rule the shuffle asks (canShuffleUnseeded)', () => {
    const list = players([null, 3]);
    expect(canShuffleUnseeded(list)).toBe(false);
    expect(notShuffledWarning({ status: 'setup', format: 'pools', players: list })).toBeNull();
    expect(canShuffleUnseeded(players([null, null]))).toBe(true);
  });
});

// The page holds poolMatches as normalizeCompetitionDetail gives them and
// patches them with each match_updated push (admin.jsx, patch.jsx applyPatch);
// the tab is given what that yields.
describe('AdminExport results button follows pushed results', () => {
  const wireDetail = () => normalizeCompetitionDetail({
    config: { id: 'c1', name: 'Open Men', status: 'pools', format: 'pools', players: players([null, null]) },
    poolMatches: [{ id: 'Pool A-1', sideA: 'P0', sideB: 'P1', sideAId: 'p0', sideBId: 'p1', status: 'scheduled' }],
    bracket: null,
  });
  const push = (detail, status) => applyPatch(detail, {
    type: 'match_updated',
    data: { competitionId: 'c1', result: { id: 'Pool A-1', sideA: 'P0', sideB: 'P1', sideAId: 'p0', sideBId: 'p1', status } },
  });
  const tab = (detail) => <AdminExport c={detail.config} t={{ id: 't1' }} password="pw" poolMatches={detail.poolMatches} bracket={detail.bracket} />;

  it('appears with the first recorded result and goes when it is cleared', () => {
    let detail = wireDetail();
    const { container, rerender } = render(tab(detail));
    expect(labels(container)).not.toContain('Download results (.xlsx)');

    detail = push(detail, 'running');
    rerender(tab(detail));
    expect(labels(container)).toContain('Download results (.xlsx)');

    detail = push(detail, 'scheduled');
    rerender(tab(detail));
    expect(labels(container)).not.toContain('Download results (.xlsx)');
    expect(labels(container)).toContain('Download blank template (.xlsx)');
  });
});
