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
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { readCode } from '../helpers/source.js';

const page = { props: null };
function ProbeViewerCompetition(props) {
  page.props = props;
  return <div data-testid="viewer-competition" />;
}

const detail = (modifiedAt, ipponsA) => ({
  config: { id: 'c1', name: 'Open', kind: 'individual', format: 'mixed', status: 'pools', courts: ['A'] },
  pools: [],
  poolMatches: [{ id: 'Pool A-0', status: 'running', court: 'A', modifiedAt, ipponsA, ipponsB: [] }],
  standings: {},
  bracket: null,
});

const sse = { emit: null };
const details = [];

const STUBBED_GLOBALS = {
  ViewerCompetition: ProbeViewerCompetition,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'T', mode: 'self-run', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => details.shift()),
    subscribeToEvents: vi.fn((onEvent) => { sse.emit = onEvent; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

// Lets every pending fetch and zero-delay timer run inside act().
const settle = () => act(async () => { await new Promise((r) => setTimeout(r, 20)); });

let restoreGlobals;
let root;
const startPath = window.location.pathname;

beforeAll(() => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  root = document.createElement('div');
  root.id = 'root';
  document.body.appendChild(root);
  window.history.pushState(null, '', '/competition/c1');
});

afterAll(() => {
  restoreGlobals();
  root.remove();
  window.history.pushState(null, '', startPath);
});

describe('the public competition page keeps the newer live score over a stale refetch (bc-dhas)', () => {
  it('a refetch that answers late with an older running match does not replace the newer one', async () => {
    // The page opens on a copy that already holds the newer point.
    details.push(detail(300, ['M', 'K']));
    await act(async () => { await import('../../app.jsx'); });
    await settle();
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

  // The event above reaches one of the page's refetches; the others sit in
  // their own branches of App's event handler, so they are read from source:
  // none may hand a fetched detail to the page's state whole.
  it('no write to the page\'s competition takes a fetched detail whole', () => {
    const code = readCode('app.jsx');
    expect(code).not.toMatch(/\.then\(\s*setSelectedCompData\s*\)/);
    const writes = [...code.matchAll(/setSelectedCompData\(([^;]*)\);/g)].map((m) => m[1].replace(/\s+/g, ' '));
    expect(writes.length).toBeGreaterThan(0);
    for (const w of writes) {
      expect(w, `setSelectedCompData(${w})`).toMatch(/^(null|prev => patchCompetitionData\(prev, event\)|\(prev\) => keepNewerDetail\(prev, data\))$/);
    }
  });
});
