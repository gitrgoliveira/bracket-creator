// bc-dhas: the public competition page's refetches never put a live score
// back to an older state. Since self-run, a score editor reads that page's
// data (MatchViewerModal opens it from any of its tabs), and a refetch can
// read the data just before a write commits and answer after a newer copy has
// landed. Taken whole, it put the older scoreline back under the open editor,
// which adopted it and wrote the lost point away on its next save. The page's
// refetches now merge through keepNewerDetail (patch.jsx), as the admin's do.
//
// Mounts the real App on /competition/c1 (app.jsx renders itself into #root
// when it is imported) with a stub API and a props probe in place of the
// competition page, then lets a stale refetch answer after a newer one.
import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, afterAll } from 'vitest';
import { mountApp, propsProbe } from '../helpers/mount_app.js';
import { readCode } from '../helpers/source.js';
import { HELD_PUSH_WAIT_MS } from '../../patch.jsx';

// settle: past the shortest delay the page's refetch gate draws (the start of
// its window, HELD_PUSH_WAIT_MS, createRefetchGate in patch.jsx).
const settle = () => act(async () => { await new Promise((r) => setTimeout(r, HELD_PUSH_WAIT_MS + 20)); });

const page = propsProbe(() => <div data-testid="viewer-competition" />);

const detail = (modifiedAt, ipponsA) => ({
  config: { id: 'c1', name: 'Open', kind: 'individual', format: 'mixed', status: 'pools', courts: ['A'] },
  pools: [],
  poolMatches: [{ id: 'Pool A-0', status: 'running', court: 'A', modifiedAt, ipponsA, ipponsB: [] }],
  standings: {},
  bracket: null,
});

const sse = { emit: null };
// The outbox's local match patches (subscribeLocalMatchPatch), as the page subscribes.
const local = { emit: null };
const details = [];

const STUBBED_GLOBALS = {
  ViewerCompetition: page.Component,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'T', mode: 'self-run', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => details.shift()),
    subscribeToEvents: vi.fn((onEvent) => { sse.emit = onEvent; return () => {}; }),
    subscribeLocalMatchPatch: vi.fn((cb) => { local.emit = cb; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;

afterAll(() => { unmount(); });

describe('the public competition page keeps the newer live score over a stale refetch (bc-dhas)', () => {
  it('a refetch that answers late with an older running match does not replace the newer one', async () => {
    // The page opens on a copy that already holds the newer point.
    details.push(detail(300, ['M', 'K']));
    ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
    expect(page.props, 'the competition page rendered').toBeTruthy();
    expect(page.props.poolMatches[0].ipponsA).toEqual(['M', 'K']);

    // A server event refetches the competition; the answer read the data
    // before the newer write committed.
    details.push(detail(200, ['M']));
    const random = vi.spyOn(Math, 'random').mockReturnValue(0);
    try {
      await act(async () => { sse.emit({ type: 'schedule_updated' }); });
      await settle();
    } finally {
      random.mockRestore();
    }
    expect(window.API.fetchCompetitionDetails).toHaveBeenCalledTimes(2);
    expect(page.props.poolMatches[0]).toMatchObject({ modifiedAt: 300, ipponsA: ['M', 'K'] });

    // A newer answer still replaces it.
    details.push(detail(400, ['M', 'K', 'D']));
    const random2 = vi.spyOn(Math, 'random').mockReturnValue(0);
    try {
      await act(async () => { sse.emit({ type: 'schedule_updated' }); });
      await settle();
    } finally {
      random2.mockRestore();
    }
    expect(page.props.poolMatches[0]).toMatchObject({ modifiedAt: 400, ipponsA: ['M', 'K', 'D'] });
  });

  // Push-wins (createPushLedger, patch.jsx): a refetch sent before a write and
  // answered from data read before it committed lands after that write's push;
  // the pushed copy stands when the fetched one has no scoreVersion or a lower
  // one (journeys/league-team: a Start flashed back to scheduled).
  it('a refetch sent before a push and answered stale keeps the pushed copy', async () => {
    let answerStale;
    details.push(new Promise((resolve) => { answerStale = resolve; }));
    const sent = window.API.fetchCompetitionDetails.mock.calls.length;
    const random = vi.spyOn(Math, 'random').mockReturnValue(0);
    try {
      await act(async () => { sse.emit({ type: 'schedule_updated' }); });
      await settle();
      expect(window.API.fetchCompetitionDetails.mock.calls.length, 'the refetch is out').toBe(sent + 1);
      const pushed = { id: 'Pool A-0', status: 'running', court: 'A', modifiedAt: 500, scoreVersion: 5, ipponsA: ['M'], ipponsB: ['K'] };
      details.push({ ...detail(500, ['M']), poolMatches: [pushed] });
      await act(async () => { sse.emit({ type: 'match_updated', data: { competitionId: 'c1', result: pushed } }); });
      await settle();
      expect(page.props.poolMatches[0]).toMatchObject({ status: 'running', scoreVersion: 5 });
      await act(async () => {
        answerStale({ ...detail(0, []), poolMatches: [{ id: 'Pool A-0', status: 'scheduled', court: 'A', ipponsA: [], ipponsB: [] }] });
      });
      await settle();
    } finally {
      random.mockRestore();
    }
    expect(page.props.poolMatches[0]).toMatchObject({ status: 'running', scoreVersion: 5, ipponsB: ['K'] });
  });

  // bc-sseq FR6: a self-run participant's write the server held answers with
  // the match as stored, and the outbox shows it to the page as a whole-match
  // local patch before the sheet's note, so the two agree. A whole patch older
  // than the copy the page holds changes nothing; one for another competition
  // (match ids repeat across competitions) never touches this one.
  it('a refused write\'s stored match reaches the open competition, never an older one or another competition', async () => {
    expect(local.emit, 'the page subscribes to the outbox\'s local patches').toBeTypeOf('function');
    const before = page.props.poolMatches[0];
    expect(before).toMatchObject({ status: 'running', scoreVersion: 5 });
    const patch = (competitionId, result) => act(async () => {
      local.emit({ type: 'match_updated', local: true, whole: true, data: { competitionId, matchId: 'Pool A-0', result: { id: 'Pool A-0', ...result } } });
    });
    await patch('c2', { status: 'completed', scoreVersion: 9 });
    expect(page.props.poolMatches[0], 'another competition\'s match of the same id').toMatchObject({ status: 'running', scoreVersion: 5 });
    await patch('c1', { status: 'completed', scoreVersion: 4, modifiedAt: 900 });
    expect(page.props.poolMatches[0], 'an older stored copy').toMatchObject({ status: 'running', scoreVersion: 5 });
    await patch('c1', { status: 'completed', scoreVersion: 6, modifiedAt: 600, ipponsB: ['K', 'M'] });
    expect(page.props.poolMatches[0]).toMatchObject({ status: 'completed', scoreVersion: 6, ipponsB: ['K', 'M'] });
  });

  // The event above reaches one of the page's refetches; the others sit in
  // their own branches of App's event handler, so they are read from source:
  // none may hand a fetched detail to the page's state whole.
  it('no write to the page\'s competition takes a fetched detail whole', () => {
    const code = readCode('app.jsx');
    expect(code).not.toMatch(/\.then\(\s*setSelectedCompData\s*\)/);
    const writes = [...code.matchAll(/setSelectedCompData\(([^;]*)\);/g)].map((m) => m[1].replace(/\s+/g, ' '));
    expect(writes.length).toBeGreaterThan(0);
    for (const w of writes) {
      expect(w, `setSelectedCompData(${w})`).toMatch(/^(null|prev => patchCompetitionData\(prev, event\)|prev => \(prev && prev\.config && prev\.config\.id === cid \? patchCompetitionData\(prev, event\) : prev\)|prev => compPushes\.push\(prev, viewerCompId, event\)|\(prev\) => keep\(prev, data\))$/);
    }
    // Every keeper is the ledger's, made as its fetch is sent.
    const keepers = [...code.matchAll(/const keep = ([^;]*);/g)].map((m) => m[1]);
    expect(keepers.length).toBeGreaterThan(0);
    for (const k of keepers) expect(k).toBe('compPushes.detail()');
  });
});
