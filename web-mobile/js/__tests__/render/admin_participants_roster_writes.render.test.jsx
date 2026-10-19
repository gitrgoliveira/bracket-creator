// Stage 6 review 10, M2 and m11: every roster PUT the participants card makes
// has ONE owner. They go one at a time, each built as it goes from the latest
// known roster (the last answered PUT, with every seed typed before it laid
// on), never from the roster as rendered. Typing a seed and then dragging a
// row, shuffling or clearing before the refetch shows the seed never sends the
// old seed over it. A typed seed is held by its competitor, not by its row.
import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';

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

// A host whose server stores each PUT as it is answered (in the order they
// land) and whose page shows each PUT's stored answer, as AdminApp's
// updateCompetition does (stage 6 review 10, m10), with no refetch (the
// refetch after a PUT can fail, or come late).
function makeServer(initial) {
  const server = { stored: initial.players, saves: [], setC: null };
  server.Host = function Host() {
    const [c, setC] = React.useState(initial);
    server.setC = setC;
    const onUpdate = async (next) => {
      const d = deferred();
      server.saves.push({ next, d });
      await d.promise;
      server.stored = next.players;
      setC((prev) => ({ ...prev, players: next.players }));
      return [];
    };
    return (
      <AdminParticipants c={c} tournament={{ name: 'Spring Taikai', courts: ['A'] }}
        onUpdate={onUpdate} password="" showToast={() => {}} onSection={() => {}} />
    );
  };
  // Answers every PUT still out, in the order sent, until none is left.
  server.answerAll = async () => {
    let done = 0;
    while (done < server.saves.length) {
      await act(async () => { server.saves[done].d.resolve(); });
      done++;
      await flush();
    }
  };
  return server;
}

const comp = (players) => ({
  id: 'c1', name: 'Autumn Cup', status: 'setup', format: 'mixed', kind: 'individual',
  poolSize: 4, poolWinners: 2, checkInEnabled: false, withZekkenName: false, numberPrefix: 'K',
  players,
});

const seeds = (players) => players.map((p) => [p.name, p.seed ?? null]);

function typeSeed(r, name, val) {
  const box = r.getByLabelText(`Seed rank for ${name}`);
  fireEvent.focus(box);
  fireEvent.change(box, { target: { value: val } });
  fireEvent.blur(box);
}

describe('the participants card\'s roster writes', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

  it('a row moved after a seed was typed keeps that seed', async () => {
    const server = makeServer(comp([
      { id: 'p-1', name: 'Alice', dojo: 'D1', seed: 1 },
      { id: 'p-2', name: 'Bob', dojo: 'D2', seed: 2 },
      { id: 'p-3', name: 'Carol', dojo: 'D3' },
    ]));
    let r;
    await act(async () => { r = render(<server.Host />); });
    typeSeed(r, 'Carol', '3');
    await flush();
    // Move Bob up before the refetch shows Carol's seed.
    fireEvent.click(r.getAllByLabelText('Move up')[1]);
    await flush();
    await server.answerAll();
    expect(seeds(server.stored), 'the move lands over the typed seed, never over the old one')
      .toEqual([['Bob', 1], ['Alice', 2], ['Carol', 3]]);
  });

  it('a shuffle after a seed was typed leaves the typed seed seeded', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const server = makeServer(comp([
      { id: 'p-1', name: 'Alice', dojo: 'D1', seed: 1 },
      { id: 'p-2', name: 'Bob', dojo: 'D2' },
      { id: 'p-3', name: 'Carol', dojo: 'D3' },
      { id: 'p-4', name: 'Dave', dojo: 'D4' },
    ]));
    let r;
    await act(async () => { r = render(<server.Host />); });
    typeSeed(r, 'Carol', '2');
    await flush();
    fireEvent.click(r.getByText('Shuffle unseeded'));
    await flush();
    await server.answerAll();
    const stored = new Map(seeds(server.stored));
    expect(stored.get('Carol'), 'the shuffle carries the typed seed').toBe(2);
    expect(server.stored.findIndex((p) => p.name === 'Carol'), 'a seeded row is not shuffled').toBe(2);
  });

  it('Clear seeds after a seed was typed clears it too, whatever order the answers come in', async () => {
    const server = makeServer(comp([
      { id: 'p-1', name: 'Alice', dojo: 'D1', seed: 1 },
      { id: 'p-2', name: 'Bob', dojo: 'D2' },
    ]));
    let r;
    await act(async () => { r = render(<server.Host />); });
    typeSeed(r, 'Bob', '2');
    await flush();
    fireEvent.click(r.getByText('Clear seeds'));
    await flush();
    // The network answers the latest request first, if more than one is out.
    while (server.saves.some((s) => !s.done)) {
      const open = server.saves.filter((s) => !s.done);
      const last = open[open.length - 1];
      last.done = true;
      await act(async () => { last.d.resolve(); });
      await flush();
    }
    expect(seeds(server.stored)).toEqual([['Alice', null], ['Bob', null]]);
  });

  // m11: a competitor without an id is held by name and dojo, never by row.
  it('a seed typed for an id-less row stays with that competitor when the rows move', async () => {
    const server = makeServer(comp([
      { name: 'Alice', dojo: 'D1' },
      { name: 'Bob', dojo: 'D2' },
      { name: 'Carol', dojo: 'D3' },
    ]));
    let r;
    await act(async () => { r = render(<server.Host />); });
    typeSeed(r, 'Alice', '1');
    await flush();
    typeSeed(r, 'Bob', '2');
    await flush();
    // Another desk moves the rows while the first seed is out.
    await act(async () => {
      server.setC(comp([
        { name: 'Bob', dojo: 'D2' },
        { name: 'Alice', dojo: 'D1' },
        { name: 'Carol', dojo: 'D3' },
      ]));
    });
    await server.answerAll();
    const stored = new Map(seeds(server.stored));
    expect(stored.get('Alice')).toBe(1);
    expect(stored.get('Bob')).toBe(2);
  });

  // Stage 6 review 11, C: a tap on Move up or Move down means "put this row
  // directly above (or below) the row it passed, as the operator saw them",
  // applied to the latest roster, and nothing when that already holds. Re-found
  // by key and spliced by index, a second Move up on Carol before the page
  // showed the first moved her back down.
  const names = (players) => players.map((p) => p.name);
  const three = () => comp([
    { id: 'p-1', name: 'Alice', dojo: 'D1', seed: 1 },
    { id: 'p-2', name: 'Bob', dojo: 'D2', seed: 2 },
    { id: 'p-3', name: 'Carol', dojo: 'D3', seed: 3 },
  ]);

  it('two quick Move up taps on one row leave it above the row it passed', async () => {
    const server = makeServer(three());
    let r;
    await act(async () => { r = render(<server.Host />); });
    fireEvent.click(r.getAllByLabelText('Move up')[2]);
    await flush();
    fireEvent.click(r.getAllByLabelText('Move up')[2]);
    await flush();
    await server.answerAll();
    expect(names(server.stored)).toEqual(['Alice', 'Carol', 'Bob']);
    expect(seeds(server.stored)).toEqual([['Alice', 1], ['Carol', 2], ['Bob', 3]]);
  });

  it('two quick Move down taps on one row leave it below the row it passed', async () => {
    const server = makeServer(three());
    let r;
    await act(async () => { r = render(<server.Host />); });
    fireEvent.click(r.getAllByLabelText('Move down')[0]);
    await flush();
    fireEvent.click(r.getAllByLabelText('Move down')[0]);
    await flush();
    await server.answerAll();
    expect(names(server.stored)).toEqual(['Bob', 'Alice', 'Carol']);
  });

  it('Move up then Move down on one row before the page shows the first leaves it below the row the second passed', async () => {
    const server = makeServer(three());
    let r;
    await act(async () => { r = render(<server.Host />); });
    fireEvent.click(r.getAllByLabelText('Move up')[1]);
    await flush();
    fireEvent.click(r.getAllByLabelText('Move down')[1]);
    await flush();
    await server.answerAll();
    expect(names(server.stored)).toEqual(['Alice', 'Carol', 'Bob']);
  });

  it('moves of two different rows each put their row where the operator put it', async () => {
    const server = makeServer(three());
    let r;
    await act(async () => { r = render(<server.Host />); });
    fireEvent.click(r.getAllByLabelText('Move up')[2]);
    await flush();
    fireEvent.click(r.getAllByLabelText('Move up')[1]);
    await flush();
    await server.answerAll();
    expect(names(server.stored)).toEqual(['Bob', 'Alice', 'Carol']);
  });
});
