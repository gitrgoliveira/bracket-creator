// Stage 6 review 12, C1: a roster PUT names the version of the roster it was
// built on, and the server refuses it (roster_changed) with the roster as it
// stands when another desk changed it meanwhile. The page shows that roster at
// once, lays the operator's change on it and sends it again, so neither desk's
// change is lost and nothing waits on the operator. Apply changes (a whole
// pasted list) is not laid on a list its operator never saw: it is told, and
// the pasted list stays in the box.
import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { installParticipantsHarness } from './admin_participants_mount_harness.jsx';
import { ROSTER_CHANGED_BEFORE_APPLY } from '../../admin_participants.jsx';

installParticipantsHarness();

async function flush() {
  await act(async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); });
}

// A server holding the roster and its version, as SaveRoster does, and a host
// that shows each stored answer and a refusal's roster, as AdminApp's
// updateCompetition does.
function makeServer(players) {
  const server = { stored: players, version: 1, puts: [], toasts: [] };
  server.Host = function Host() {
    const [c, setC] = React.useState({
      id: 'c1', name: 'Autumn Cup', status: 'setup', format: 'mixed', kind: 'individual',
      poolSize: 4, poolWinners: 2, checkInEnabled: false, withZekkenName: false, numberPrefix: 'K',
      players, rosterVersion: 'v1',
    });
    server.setC = setC;
    const onUpdate = async (next) => {
      server.puts.push(next);
      if (next.rosterVersion !== `v${server.version}`) {
        const e = new Error('changed');
        e.code = 'roster_changed';
        e.players = server.stored;
        e.rosterVersion = `v${server.version}`;
        setC((prev) => ({ ...prev, players: server.stored, rosterVersion: `v${server.version}` }));
        throw e;
      }
      server.stored = next.players;
      server.version += 1;
      setC((prev) => ({ ...prev, players: next.players, rosterVersion: `v${server.version}` }));
      const answer = [];
      Object.defineProperty(answer, 'rosterVersion', { value: `v${server.version}` });
      return answer;
    };
    return (
      <window.AdminParticipants c={c} tournament={{ name: 'Spring Taikai', courts: ['A'] }}
        onUpdate={onUpdate} password="" showToast={(m, t) => server.toasts.push({ m, t })} onSection={() => {}} />
    );
  };
  // Another desk's change, made without this page knowing.
  server.elsewhere = (next) => { server.stored = next; server.version += 1; };
  return server;
}

function typeSeed(r, name, val) {
  const box = r.getByLabelText(`Seed rank for ${name}`);
  fireEvent.focus(box);
  fireEvent.change(box, { target: { value: val } });
  fireEvent.blur(box);
}

const ROSTER = [
  { id: 'p-1', name: 'Alice', dojo: 'D1' },
  { id: 'p-2', name: 'Bob', dojo: 'D2' },
  { id: 'p-3', name: 'Carol', dojo: 'D3' },
];

describe('the participants card when another desk changed the roster', () => {
  let savedConfirm;
  beforeEach(() => { savedConfirm = window.confirmDialog; });
  afterEach(() => { window.confirmDialog = savedConfirm; vi.restoreAllMocks(); });

  it('a typed seed lands on the roster another desk added to, and the added competitor stays', async () => {
    const server = makeServer(ROSTER);
    let r;
    await act(async () => { r = render(<server.Host />); });
    server.elsewhere([...ROSTER, { id: 'p-4', name: 'Eve', dojo: 'D4' }]);
    typeSeed(r, 'Carol', '1');
    await flush();
    expect(server.puts.length, 'refused once, sent again at once').toBe(2);
    expect(server.puts[0].rosterVersion).toBe('v1');
    expect(server.puts[1].rosterVersion).toBe('v2');
    expect(server.stored.map((p) => [p.name, p.seed ?? null])).toEqual([
      ['Alice', null], ['Bob', null], ['Carol', 1], ['Eve', null],
    ]);
    expect(r.getByText('Eve'), 'the page shows the roster as it stands').toBeTruthy();
    expect(server.toasts, 'nothing to tell: both changes landed').toEqual([]);
  });

  it('two desks seeding different competitors both land', async () => {
    const server = makeServer(ROSTER);
    let r;
    await act(async () => { r = render(<server.Host />); });
    server.elsewhere([{ ...ROSTER[0], seed: 1 }, ROSTER[1], ROSTER[2]]);
    typeSeed(r, 'Bob', '2');
    await flush();
    expect(server.stored.map((p) => [p.name, p.seed ?? null])).toEqual([
      ['Alice', 1], ['Bob', 2], ['Carol', null],
    ]);
  });

  it('a competitor another desk removed is not brought back by a seed typed before it', async () => {
    const server = makeServer(ROSTER);
    let r;
    await act(async () => { r = render(<server.Host />); });
    server.elsewhere([ROSTER[0], ROSTER[2]]);
    typeSeed(r, 'Carol', '1');
    await flush();
    expect(server.stored.map((p) => p.name)).toEqual(['Alice', 'Carol']);
    expect(server.stored[1].seed).toBe(1);
  });

  it('Apply changes over a list changed on another desk saves nothing, says so, and keeps the pasted list', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    const server = makeServer(ROSTER);
    let r;
    await act(async () => { r = render(<server.Host />); });
    const box = r.container.querySelector('textarea');
    await act(async () => { fireEvent.change(box, { target: { value: 'Alice, D1\nBob, D2' } }); });
    server.elsewhere([...ROSTER, { id: 'p-4', name: 'Eve', dojo: 'D4' }]);
    const apply = Array.from(r.container.querySelectorAll('button')).find((b) => b.textContent === 'Apply changes');
    await act(async () => { fireEvent.click(apply); });
    await flush();
    expect(server.puts.length, 'not laid on a list the operator never saw').toBe(1);
    expect(server.stored.map((p) => p.name)).toEqual(['Alice', 'Bob', 'Carol', 'Eve']);
    expect(server.toasts).toEqual([{ m: ROSTER_CHANGED_BEFORE_APPLY, t: 'error' }]);
    expect(r.container.querySelector('textarea').value).toBe('Alice, D1\nBob, D2');
  });
});
