// Stage 6 review 11, A: every roster PUT of the participants card is built
// from the roster the page shows (the PUT's stored answer, then the refetch,
// both shown before the save resolves, as AdminApp's updateCompetition does),
// with the seeds typed and still out laid on. A change another desk made
// while a PUT was out (a check-in, a competitor added or removed) is on the
// page once the refetch shows it, and the next PUT of every kind keeps it:
// the PUT is a whole-roster write, and the server stores `checkedIn` as sent
// (SaveParticipants writes the column from the body, with no carry), so a
// PUT built from the earlier payload unchecked Bob or brought Dave back.
import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest';

function deferred() {
  let resolve, reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

async function flush() {
  await act(async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); });
}

let AdminParticipants = null;
beforeAll(async () => {
  await import('../../admin_participants.jsx');
  AdminParticipants = window.AdminParticipants;
});

const comp = (players) => ({
  id: 'c1', name: 'Autumn Cup', status: 'setup', format: 'mixed', kind: 'individual',
  poolSize: 4, poolWinners: 2, checkInEnabled: true, withZekkenName: false, numberPrefix: 'K',
  players,
});

// A host shaped like AdminApp's updateCompetition: the PUT is answered with
// the roster as stored, which the page shows at once (m10); then the refetch
// shows the server as it is now (a desk change that landed after the PUT
// included); the save resolves only once the page has rendered that.
function makeServer(initial) {
  const server = { state: initial.players.map((p) => ({ ...p })), puts: [], steps: [], deskChange: null };
  const step = () => { const d = deferred(); server.steps.push(d); return d.promise; };
  server.Host = function Host() {
    const [c, setC] = React.useState(initial);
    const onUpdate = async (next) => {
      server.puts.push(next.players.map((p) => ({ ...p })));
      await step(); // the PUT's answer
      server.state = next.players.map((p) => ({ ...p }));
      const answer = server.state;
      if (server.deskChange) { server.state = server.deskChange(server.state); server.deskChange = null; }
      setC((prev) => ({ ...prev, players: answer }));
      await step(); // the refetch's answer
      const now = server.state;
      setC((prev) => ({ ...prev, players: now }));
      await step(); // rendered before the save resolves
      return [];
    };
    return (
      <AdminParticipants c={c} tournament={{ name: 'Spring Taikai', courts: ['A'] }}
        onUpdate={onUpdate} password="" showToast={() => {}} onSection={() => {}} />
    );
  };
  // Answers every step still open, one act each, so each render lands
  // before the next step goes on.
  server.settle = async () => {
    while (server.steps.length) {
      const d = server.steps.shift();
      await act(async () => { d.resolve(); });
      await flush();
    }
  };
  return server;
}

const roster = () => comp([
  { id: 'p-1', name: 'Alice', dojo: 'D1', seed: 1 },
  { id: 'p-2', name: 'Bob', dojo: 'D2', checkedIn: false },
  { id: 'p-3', name: 'Carol', dojo: 'D3' },
  { id: 'p-4', name: 'Dave', dojo: 'D4' },
  { id: 'p-6', name: 'Frank', dojo: 'D6' },
]);

const DESK = {
  'checks Bob in': {
    change: (ps) => ps.map((p) => (p.name === 'Bob' ? { ...p, checkedIn: true } : p)),
    kept: (ps) => (ps.find((p) => p.name === 'Bob') || {}).checkedIn === true,
  },
  'adds Eve': {
    change: (ps) => [...ps, { id: 'p-5', name: 'Eve', dojo: 'D5' }],
    kept: (ps) => ps.some((p) => p.name === 'Eve'),
  },
  'removes Dave': {
    change: (ps) => ps.filter((p) => p.name !== 'Dave'),
    kept: (ps) => !ps.some((p) => p.name === 'Dave'),
  },
};

function typeSeed(r, name, val) {
  const box = r.getByLabelText(`Seed rank for ${name}`);
  fireEvent.focus(box);
  fireEvent.change(box, { target: { value: val } });
  fireEvent.blur(box);
}

const ACTIONS = {
  'a typed seed': async (r) => { typeSeed(r, 'Bob', '2'); },
  'a row moved': async (r) => { fireEvent.click(r.getAllByLabelText('Move down')[0]); },
  'a shuffle': async (r) => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    fireEvent.click(r.getByText('Shuffle unseeded'));
  },
  'Clear seeds': async (r) => { fireEvent.click(r.getByText('Clear seeds')); },
  'a seed file': async (r) => {
    const input = r.container.querySelector('input[type="file"]');
    const file = new File(['Alice, 2\n'], 'seeds.csv', { type: 'text/csv' });
    await act(async () => { fireEvent.change(input, { target: { files: [file] } }); });
    // FileReader answers on its own turn.
    for (let i = 0; i < 20 && !r.__putSeen(); i++) await act(async () => { await new Promise((res) => setTimeout(res, 5)); });
  },
};

afterEach(() => { vi.restoreAllMocks(); });

describe('the participants card builds every roster PUT from the roster it shows', () => {
  for (const [deskName, desk] of Object.entries(DESK)) {
    for (const [actionName, action] of Object.entries(ACTIONS)) {
      it(`${actionName} after the desk ${deskName} while a seed was saving keeps that change`, async () => {
        const server = makeServer(roster());
        let r;
        await act(async () => { r = render(<server.Host />); });
        r.__putSeen = () => server.puts.length >= 2;
        typeSeed(r, 'Carol', '3');
        await flush();
        expect(server.puts.length).toBe(1);
        // The desk's change lands on the server after the seed's PUT.
        server.deskChange = desk.change;
        await server.settle();
        expect(desk.kept(server.state), 'the refetch the page shows holds the desk change').toBe(true);
        await action(r);
        await flush();
        expect(server.puts.length, 'the action sent its PUT').toBe(2);
        expect(desk.kept(server.puts[1]), `the ${actionName} PUT keeps what the desk did`).toBe(true);
        await server.settle();
        expect(desk.kept(server.state)).toBe(true);
      });
    }
  }

  it('Apply changes after the desk checked Bob in sends no check-in of its own, so the one the desk stored stands', async () => {
    const savedConfirm = window.confirmDialog;
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    try {
      const server = makeServer(roster());
      let r;
      await act(async () => { r = render(<server.Host />); });
      // Seed 2, so the seeding stays contiguous and Apply is offered.
      typeSeed(r, 'Carol', '2');
      await flush();
      server.deskChange = DESK['checks Bob in'].change;
      await server.settle();
      const box = r.container.querySelector('textarea');
      await act(async () => { fireEvent.change(box, { target: { value: 'Alice, D1\nBob, D2\nCarol, D3\nDave, D4\nFrank, D6' } }); });
      const apply = Array.from(r.container.querySelectorAll('button')).find((b) => b.textContent === 'Apply changes');
      await act(async () => { fireEvent.click(apply); });
      await flush();
      expect(server.puts.length).toBe(2);
      // A roster save never writes a check-in: Bob is on the list, and the
      // PUT says nothing about whether he is checked in (the server keeps the
      // check-in it stored for him; stage 6 review 13, m4).
      const sentBob = server.puts[1].find((p) => p.name === 'Bob');
      expect(sentBob, 'Bob is still on the list').toBeTruthy();
      expect('checkedIn' in sentBob, 'and no check-in rides on him').toBe(false);
      await server.settle();
    } finally {
      window.confirmDialog = savedConfirm;
    }
  });

  it('a seed typed while the first is out, the desk checking Bob in meanwhile, keeps both seeds and the check-in', async () => {
    const server = makeServer(roster());
    let r;
    await act(async () => { r = render(<server.Host />); });
    typeSeed(r, 'Carol', '3');
    await flush();
    typeSeed(r, 'Dave', '4');
    await flush();
    server.deskChange = DESK['checks Bob in'].change;
    await server.settle();
    expect(server.puts.length).toBe(2);
    const second = server.puts[1];
    expect(second.find((p) => p.name === 'Bob').checkedIn).toBe(true);
    expect(second.find((p) => p.name === 'Carol').seed).toBe(3);
    expect(second.find((p) => p.name === 'Dave').seed).toBe(4);
  });
});
