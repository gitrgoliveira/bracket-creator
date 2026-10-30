// Stage 6 review 9 (F1, F2): a box shows what the operator typed while that
// change is waiting, queued or out, and once its save settles it shows what is
// stored: the new value when the save was taken, the stored value when it was
// refused (the operator is told by the save's own refusal path). A change made
// elsewhere while nothing of the operator's is waiting shows at once, or on
// blur when the box has the focus.
import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { StableInput } from '../../ui.jsx';

function deferred() {
  let resolve, reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

// A parent that stores the value only once its save is taken, as admin.jsx's
// updateCompetition does (no optimistic copy): a refused save leaves the
// stored value as it was and tells the operator.
// `onSetter` hands the test the stored value's setter, for a store that moves
// on its own (Host.setStored).
function Host({ initial, saves, told, onSetter }) {
  const [stored, setStored] = React.useState(initial);
  const onChange = (val) => {
    const d = deferred();
    saves.push({ val, d });
    return d.promise.then(
      () => { setStored(val); },
      (err) => { told(err.message); },
    );
  };
  if (onSetter) onSetter(setStored);
  return <StableInput aria-label="Seed" type="number" value={stored} onChange={onChange} autoSelect={false} />;
}
const keepSetter = (set) => { Host.setStored = set; };

async function flush() {
  await act(async () => { await Promise.resolve(); await Promise.resolve(); });
}

describe('StableInput follows its save', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it('keeps showing a change whose save is out after the box loses focus (F1)', async () => {
    const saves = [];
    const { getByLabelText } = render(<Host initial={2} saves={saves} told={() => {}} />);
    const box = getByLabelText('Seed');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '3' } });
    await act(async () => { vi.advanceTimersByTime(250); });
    expect(saves).toHaveLength(1);
    fireEvent.blur(box);
    expect(box.value, 'the change in flight, not the old stored value').toBe('3');
    saves[0].d.resolve();
    await flush();
    expect(box.value).toBe('3');
  });

  it('shows the stored value once a save sent on blur is refused, and the operator is told (F2)', async () => {
    const saves = [];
    const told = vi.fn();
    const { getByLabelText } = render(<Host initial={2} saves={saves} told={told} />);
    const box = getByLabelText('Seed');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '5' } });
    fireEvent.blur(box);
    expect(saves.map((s) => s.val)).toEqual([5]);
    expect(box.value, 'the change while its save is out').toBe('5');
    saves[0].d.reject(new Error('Seed 5 is out of range'));
    await flush();
    expect(told).toHaveBeenCalledWith('Seed 5 is out of range');
    expect(box.value, 'the stored value once refused').toBe('2');
  });

  it('shows the new value once a save sent on blur is taken', async () => {
    const saves = [];
    const { getByLabelText } = render(<Host initial={2} saves={saves} told={() => {}} />);
    const box = getByLabelText('Seed');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '5' } });
    fireEvent.blur(box);
    saves[0].d.resolve();
    await flush();
    expect(box.value).toBe('5');
  });

  it('keeps showing a queued change over a change made elsewhere until it settles', async () => {
    const saves = [];
    const { getByLabelText } = render(<Host initial={2} saves={saves} told={() => {}} onSetter={keepSetter} />);
    const box = getByLabelText('Seed');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '4' } });
    fireEvent.blur(box);
    // Another device stores 7 while this change is still queued.
    await act(async () => { Host.setStored(7); });
    expect(box.value, 'the operator\'s queued change').toBe('4');
    saves[0].d.resolve();
    await flush();
    expect(box.value).toBe('4');
  });

  it('shows a refused save\'s stored value after a refusal made while the box still had the focus', async () => {
    const saves = [];
    const told = vi.fn();
    const { getByLabelText } = render(<Host initial={2} saves={saves} told={told} />);
    const box = getByLabelText('Seed');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '3' } });
    await act(async () => { vi.advanceTimersByTime(250); });
    saves[0].d.reject(new Error('refused'));
    await flush();
    expect(told).toHaveBeenCalledTimes(1);
    fireEvent.blur(box);
    expect(saves).toHaveLength(1);
    expect(box.value).toBe('2');
  });

  it('shows a change made elsewhere at once when nothing is waiting, and on blur when the box has the focus', async () => {
    const saves = [];
    const { getByLabelText } = render(<Host initial={2} saves={saves} told={() => {}} onSetter={keepSetter} />);
    const box = getByLabelText('Seed');
    await act(async () => { Host.setStored(6); });
    expect(box.value).toBe('6');
    fireEvent.focus(box);
    await act(async () => { Host.setStored(8); });
    expect(box.value, 'not under the operator\'s focus').toBe('6');
    fireEvent.blur(box);
    expect(saves).toHaveLength(0);
    expect(box.value).toBe('8');
  });

  it('follows a parent that stores at once (a local setter returns nothing)', async () => {
    function Local() {
      const [v, setV] = React.useState('09:00');
      return <StableInput aria-label="Time" type="time" value={v} onChange={(x) => setV(x)} />;
    }
    const { getByLabelText } = render(<Local />);
    const box = getByLabelText('Time');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '10:30' } });
    fireEvent.blur(box);
    expect(box.value).toBe('10:30');
  });
});

// The seed box through the real participants card, with a parent that stores
// a competition only once its PUT is taken (admin.jsx's updateCompetition).
let AdminParticipants = null;
beforeAll(async () => {
  await import('../../admin_participants.jsx');
  AdminParticipants = window.AdminParticipants;
});

function SeedHost({ initial, saves, told }) {
  const [c, setC] = React.useState(initial);
  const onUpdate = async (next) => {
    const d = deferred();
    saves.push({ next, d });
    try {
      await d.promise;
    } catch (e) {
      told(e.message);
      throw e;
    }
    setC(next);
    return [];
  };
  return (
    <AdminParticipants c={c} tournament={{ name: 'Spring Taikai', courts: ['A'] }}
      onUpdate={onUpdate} password="" showToast={() => {}} onSection={() => {}} />
  );
}

const roster = () => ({
  id: 'c1', name: 'Autumn Cup', status: 'setup', format: 'mixed', kind: 'individual',
  poolSize: 4, poolWinners: 2, checkInEnabled: false, withZekkenName: false, numberPrefix: 'K',
  players: [
    { id: 'p-1', name: 'Alice', dojo: 'Dojo Alice' },
    { id: 'p-2', name: 'Bob', dojo: 'Dojo Bob' },
  ],
});

describe('the seed box', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it('keeps the typed seed through its save, and shows the stored one once it is refused', async () => {
    // The card logs the refused PUT (the toast is updateCompetition's).
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    const saves = [];
    const told = vi.fn();
    let r;
    await act(async () => { r = render(<SeedHost initial={roster()} saves={saves} told={told} />); });
    const box = r.getByLabelText('Seed rank for Alice');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '3' } });
    await act(async () => { vi.advanceTimersByTime(250); });
    fireEvent.blur(box);
    expect(box.value, 'F1: the seed in flight').toBe('3');
    await act(async () => { saves[0].d.resolve(); });
    await flush();
    expect(box.value).toBe('3');

    const bob = r.getByLabelText('Seed rank for Bob');
    fireEvent.focus(bob);
    fireEvent.change(bob, { target: { value: '5' } });
    fireEvent.blur(bob);
    await flush();
    expect(bob.value).toBe('5');
    await act(async () => { saves[1].d.reject(new Error('Seed 5 is out of range')); });
    await flush();
    expect(told).toHaveBeenCalledWith('Seed 5 is out of range');
    expect(bob.value, 'F2: the stored seed once refused').toBe('');
    expect(logged).toHaveBeenCalledTimes(1);
    logged.mockRestore();
  });

  it('two quick seed edits both reach the stored roster (F9)', async () => {
    const saves = [];
    let r;
    await act(async () => { r = render(<SeedHost initial={roster()} saves={saves} told={() => {}} />); });
    const alice = r.getByLabelText('Seed rank for Alice');
    const bob = r.getByLabelText('Seed rank for Bob');
    fireEvent.focus(alice);
    fireEvent.change(alice, { target: { value: '1' } });
    fireEvent.blur(alice);
    fireEvent.focus(bob);
    fireEvent.change(bob, { target: { value: '2' } });
    fireEvent.blur(bob);
    // The server stores each PUT as it is answered, in the order they go.
    await flush();
    for (let i = 0; i < saves.length; i++) {
      await act(async () => { saves[i].d.resolve(); });
      await flush();
    }
    expect(saves.length).toBeGreaterThan(0);
    const last = saves[saves.length - 1].next.players;
    expect(last.map((p) => [p.name, p.seed])).toEqual([['Alice', 1], ['Bob', 2]]);
    expect(alice.value).toBe('1');
    expect(bob.value).toBe('2');
  });
});
