// Shuffle unseeded records the shuffle: its roster PUT carries
// `rosterShuffled: true` (each retry after roster_changed too), the server sets
// the stored marker only when that roster lands, and the page shows the marker
// from the PUT's own answer at once (mergeSavedCompetition and
// storedCompetitionInDetail carry it), so the Export tab's "never shuffled"
// warning goes without a reload. Any other roster PUT sends no marker of its
// own, and a refused shuffle sets nothing.
import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { installParticipantsHarness } from './admin_participants_mount_harness.jsx';
import { mergeSavedCompetition, storedCompetitionInDetail } from '../../admin.jsx';

installParticipantsHarness();

async function flush() {
  await act(async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); });
}

const ROSTER = [
  { id: 'p-1', name: 'Alice', dojo: 'D1' },
  { id: 'p-2', name: 'Bob', dojo: 'D2' },
  { id: 'p-3', name: 'Carol', dojo: 'D3' },
];

// A server that stores the roster and, as the competition PUT does, the marker
// only when the body says true and the roster lands; its answer is the stored
// competition (the marker omitted while false, as omitempty leaves it). The
// host lays the answer on what the page shows the way AdminApp's
// updateCompetition does.
function makeServer(players) {
  const server = {
    stored: players, version: 1, puts: [], marker: false, shown: null, detail: null,
    refuseNext: null, failNext: false,
  };
  server.Host = function Host() {
    const [c, setC] = React.useState({
      id: 'c1', name: 'Autumn Cup', status: 'setup', format: 'mixed', kind: 'individual',
      poolSize: 4, poolWinners: 2, checkInEnabled: false, withZekkenName: false, numberPrefix: 'K',
      players, rosterVersion: 'v1',
    });
    server.shown = c;
    const onUpdate = async (next) => {
      server.puts.push(next);
      if (server.failNext) { server.failNext = false; throw new Error('boom'); }
      if (server.refuseNext) {
        const refusal = server.refuseNext;
        server.refuseNext = null;
        server.stored = refusal.players;
        server.version += 1;
        const e = new Error('changed');
        e.code = 'roster_changed';
        e.players = server.stored;
        e.rosterVersion = `v${server.version}`;
        setC((prev) => ({ ...prev, players: server.stored, rosterVersion: `v${server.version}` }));
        throw e;
      }
      server.stored = next.players;
      server.version += 1;
      if (next.rosterShuffled === true) server.marker = true;
      const { rosterShuffled: _held, ...stored } = c;
      const answer = { ...stored, players: server.stored, ...(server.marker ? { rosterShuffled: true } : {}) };
      setC((prev) => mergeSavedCompetition(prev, answer, next));
      server.detail = storedCompetitionInDetail({ config: { id: 'c1', courts: [] } }, 'c1', answer, next);
      const rosterAnswer = [];
      Object.defineProperty(rosterAnswer, 'rosterVersion', { value: `v${server.version}` });
      Object.defineProperty(rosterAnswer, 'rosterPlayers', { value: server.stored });
      return rosterAnswer;
    };
    return (
      <window.AdminParticipants c={c} tournament={{ name: 'Spring Taikai', courts: ['A'] }}
        onUpdate={onUpdate} password="" showToast={() => {}} onSection={() => {}} />
    );
  };
  return server;
}

describe('Shuffle unseeded records the shuffle', () => {
  let savedConfirm;
  beforeEach(() => { savedConfirm = window.confirmDialog; });
  afterEach(() => { window.confirmDialog = savedConfirm; vi.restoreAllMocks(); });

  it('sends rosterShuffled: true and the page shows the marker from the answer', async () => {
    const server = makeServer(ROSTER);
    let r;
    await act(async () => { r = render(<server.Host />); });
    expect(server.shown.rosterShuffled, 'never shuffled before').toBeFalsy();
    fireEvent.click(r.getByRole('button', { name: 'Shuffle unseeded' }));
    await flush();
    expect(server.puts).toHaveLength(1);
    expect(server.puts[0].rosterShuffled).toBe(true);
    expect(server.marker).toBe(true);
    expect(server.shown.rosterShuffled, 'the page holds the marker at once').toBe(true);
    expect(server.detail.config.rosterShuffled, 'the competition page detail too').toBe(true);
  });

  it('a retry after roster_changed carries the marker again', async () => {
    const server = makeServer(ROSTER);
    let r;
    await act(async () => { r = render(<server.Host />); });
    server.refuseNext = { players: [...ROSTER, { id: 'p-4', name: 'Eve', dojo: 'D4' }] };
    fireEvent.click(r.getByRole('button', { name: 'Shuffle unseeded' }));
    await flush();
    expect(server.puts).toHaveLength(2);
    expect(server.puts[0].rosterShuffled).toBe(true);
    expect(server.puts[1].rosterShuffled).toBe(true);
    expect(server.marker).toBe(true);
    expect(server.stored.map((p) => p.name).sort()).toEqual(['Alice', 'Bob', 'Carol', 'Eve']);
  });

  it('a refused shuffle leaves the page without the marker', async () => {
    const server = makeServer(ROSTER);
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    let r;
    await act(async () => { r = render(<server.Host />); });
    server.failNext = true;
    fireEvent.click(r.getByRole('button', { name: 'Shuffle unseeded' }));
    await flush();
    expect(server.marker).toBe(false);
    expect(server.shown.rosterShuffled).toBeFalsy();
    // The card logs the failure (the host toasts it); expected here.
    expect(logged).toHaveBeenCalled();
    logged.mockClear();
  });

  it('a seed typed by hand sends no marker of its own', async () => {
    const server = makeServer(ROSTER);
    let r;
    await act(async () => { r = render(<server.Host />); });
    const box = r.getByLabelText('Seed rank for Carol');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '1' } });
    fireEvent.blur(box);
    await flush();
    expect(server.puts.length).toBeGreaterThan(0);
    for (const body of server.puts) expect(body).not.toHaveProperty('rosterShuffled');
    expect(server.marker).toBe(false);
  });

  it('a later roster PUT echoes the marker the page holds, a no-op on one already set', async () => {
    const server = makeServer(ROSTER);
    let r;
    await act(async () => { r = render(<server.Host />); });
    fireEvent.click(r.getByRole('button', { name: 'Shuffle unseeded' }));
    await flush();
    const box = r.getByLabelText('Seed rank for Carol');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '1' } });
    fireEvent.blur(box);
    await flush();
    const last = server.puts[server.puts.length - 1];
    expect(last.rosterShuffled, 'the page echoes what it holds').toBe(true);
    expect(server.marker).toBe(true);
    expect(server.shown.rosterShuffled).toBe(true);
  });

  it('nothing is sent, and no marker set, when fewer than two are unseeded', async () => {
    const server = makeServer([
      { id: 'p-1', name: 'Alice', dojo: 'D1', seed: 1 },
      { id: 'p-2', name: 'Bob', dojo: 'D2', seed: 2 },
      { id: 'p-3', name: 'Carol', dojo: 'D3' },
    ]);
    let r;
    await act(async () => { r = render(<server.Host />); });
    fireEvent.click(r.getByRole('button', { name: 'Shuffle unseeded' }));
    await flush();
    expect(server.puts).toHaveLength(0);
    expect(server.marker).toBe(false);
  });
});
