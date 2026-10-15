// bc-evts: on the engi editor, taking a flag off and tapping it back (or the
// reverse) while the earlier tap is still saving returned the count to the
// value the server still showed, so the editor read it as untouched, and the
// earlier save's echo then wiped the second tap; its own save named no change.
// The counts now keep every server copy stamped before the operator's last
// edit, the rule the individual editor (bc-ltap) and the team sheet (bc-kclr)
// follow.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { EngiScoreEditorModal } from '../../admin_scoring_engi.jsx';

const NOW = 1_800_000_000_000;
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(NOW); });
afterEach(() => { vi.useRealTimers(); });

// A running engi pool match. No compId: the history fetch stays out.
const engi = (overrides = {}) => ({
  id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p-aka', name: 'Aka Pair 1 - Aka Pair 2', dojo: 'Aka Dojo' },
  sideB: { id: 'p-shiro', name: 'Shiro Pair 1 - Shiro Pair 2', dojo: 'Shiro Dojo' },
  flagsA: 0, flagsB: 0, modifiedAt: NOW - 5000,
  ...overrides,
});

// A save the server took: the answer a landed write comes back with.
const LANDED = { id: 'm1', status: 'running' };

const el = (match, onSubmit) => (
  <EngiScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} variant="inline" />
);

const count = (r, side) => r.getByTestId(`engi-${side}-count`).textContent;
const tap = async (r, id) => { await act(async () => { fireEvent.click(r.getByTestId(id)); }); };
const wait = async (ms) => { await act(async () => { vi.advanceTimersByTime(ms); }); };

describe('a flag tap made while the previous save is out is never wiped by that save\'s echo (bc-evts)', () => {
  it('add a flag, take it off before the add\'s echo: the count stays at the server\'s and the removal is saved', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(engi(), onSubmit)); });
    await tap(r, 'engi-aka-inc');
    await wait(350); // the add's autosave goes out
    expect(onSubmit.mock.calls.at(-1)?.[0]?.flagsA, 'precondition: the add was saved').toBe(1);

    vi.setSystemTime(NOW + 400);
    await tap(r, 'engi-aka-dec'); // back to the value the server still shows
    expect(count(r, 'aka'), 'the tap shows').toBe('0');

    // The add's echo: the server now holds the flag, stamped at the add.
    await act(async () => { r.rerender(el(engi({ flagsA: 1, modifiedAt: NOW + 50 }), onSubmit)); });
    expect(count(r, 'aka'), 'the echo of the earlier add does not bring the flag back').toBe('0');

    await wait(350); // the removal's own autosave
    const save = onSubmit.mock.calls.at(-1)?.[0];
    expect(save?.flagsA, 'the removal is sent').toBe(0);
    expect(save?.changed, 'and named as a change').toContain('flags');
  });

  it('take a flag off, tap it back before the removal\'s echo: the flag stays and is saved', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(engi({ flagsA: 1 }), onSubmit)); });
    await tap(r, 'engi-aka-dec');
    await wait(350); // the removal's autosave goes out
    expect(onSubmit.mock.calls.at(-1)?.[0]?.flagsA, 'precondition: the removal was saved').toBe(0);

    vi.setSystemTime(NOW + 400);
    await tap(r, 'engi-aka-inc'); // back to the value the server still shows
    expect(count(r, 'aka'), 'the tap shows').toBe('1');

    // The removal's echo: the server now holds no flag, stamped at the removal.
    await act(async () => { r.rerender(el(engi({ flagsA: 0, modifiedAt: NOW + 50 }), onSubmit)); });
    expect(count(r, 'aka'), 'the echo of the earlier removal does not wipe the newer tap').toBe('1');

    await wait(350); // the tap's own autosave
    const save = onSubmit.mock.calls.at(-1)?.[0];
    expect(save?.flagsA, 'the tap is sent').toBe(1);
    expect(save?.changed, 'and named as a change').toContain('flags');
  });

  it('a change made elsewhere after the last tap is still followed', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(engi(), onSubmit)); });
    await tap(r, 'engi-aka-inc');
    await wait(350);
    // The server copy carries this tap, then another device's Shiro flag,
    // stamped after the tap.
    await act(async () => { r.rerender(el(engi({ flagsA: 1, modifiedAt: NOW + 50 }), onSubmit)); });
    await act(async () => { r.rerender(el(engi({ flagsA: 1, flagsB: 1, modifiedAt: NOW + 5000 }), onSubmit)); });
    expect(count(r, 'shiro'), 'the later change shows').toBe('1');
    expect(count(r, 'aka')).toBe('1');
  });

  it('once the tap\'s own save has landed, a server copy stamped just before the tap is followed', async () => {
    // The write's stamp is worked out at send, less the edit's age, so it can
    // read a millisecond before the tap that made it. Once that save has
    // landed the board holds nothing unsent, and a change the server applied
    // under such a stamp is the match as it stands.
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(engi(), onSubmit)); });
    await tap(r, 'engi-aka-inc');
    await wait(350); // the tap's save goes out and lands
    await act(async () => {});
    const echo = engi({ flagsA: 1, modifiedAt: NOW - 1 });
    await act(async () => { r.rerender(el(echo, onSubmit)); });
    await act(async () => { r.rerender(el({ ...echo, flagsB: 1 }, onSubmit)); });
    expect(count(r, 'shiro'), 'the board took the Shiro flag stamped just before the tap').toBe('1');
  });

  it('a save built before the latest tap clears nothing when it lands', async () => {
    let landFirst;
    const first = new Promise((resolve) => { landFirst = resolve; });
    const onSubmit = vi.fn().mockReturnValueOnce(first).mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(engi(), onSubmit)); });
    await tap(r, 'engi-aka-inc');
    await wait(350); // the add's save is out and unanswered
    expect(onSubmit.mock.calls.at(-1)?.[0]?.flagsA, 'precondition: the add was saved').toBe(1);

    vi.setSystemTime(NOW + 400);
    await tap(r, 'engi-aka-dec'); // a tap the save in flight was not built with
    await act(async () => { landFirst(LANDED); });

    // The add's echo, stamped before the removal.
    await act(async () => { r.rerender(el(engi({ flagsA: 1, modifiedAt: NOW + 50 }), onSubmit)); });
    expect(count(r, 'aka'), 'the add landing does not release the board from the removal it never carried').toBe('0');

    await wait(350); // the removal's own autosave
    expect(onSubmit.mock.calls.at(-1)?.[0]?.flagsA, 'the removal is sent').toBe(0);
  });

  // A REFUSED save is not here: it takes the server's copy instead
  // (individual_engi_refused_autosave_takes_server.render.test.jsx).
  it('a queued save keeps the board against an older copy', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ queued: true });
    let r;
    await act(async () => { r = render(el(engi(), onSubmit)); });
    await tap(r, 'engi-aka-inc');
    await wait(350);
    vi.setSystemTime(NOW + 400);
    await tap(r, 'engi-aka-dec');
    await wait(350);
    expect(onSubmit.mock.calls.length, 'precondition: both taps were saved').toBe(2);

    await act(async () => { r.rerender(el(engi({ flagsA: 1, modifiedAt: NOW + 50 }), onSubmit)); });
    expect(count(r, 'aka'), 'a write that did not land released nothing').toBe('0');
  });

  it('a scheduled match is not autosaved, so a net-zero fiddle holds nothing against another device', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(engi({ status: 'scheduled' }), onSubmit)); });
    await tap(r, 'engi-aka-inc');
    await tap(r, 'engi-aka-dec');
    await wait(350);
    expect(onSubmit, 'precondition: nothing was autosaved').not.toHaveBeenCalled();

    // Another device's flag, stamped before the taps.
    await act(async () => { r.rerender(el(engi({ status: 'scheduled', flagsB: 1, modifiedAt: NOW - 1000 }), onSubmit)); });
    expect(count(r, 'shiro'), 'the copy from the other device is followed').toBe('1');
  });

  it.each([
    ['one millisecond before the tap\'s stamp is held', -1, '0'],
    ['at the tap\'s stamp is followed', 0, '1'],
  ])('a copy stamped %s', async (_name, offset, shiro) => {
    const onSubmit = vi.fn().mockResolvedValue({ queued: true });
    let r;
    await act(async () => { r = render(el(engi(), onSubmit)); });
    await tap(r, 'engi-aka-inc');
    await wait(350);
    vi.setSystemTime(NOW + 400);
    await tap(r, 'engi-aka-dec'); // the board is back at the server's value; the stamp is NOW + 400
    await act(async () => { r.rerender(el(engi({ flagsB: 1, modifiedAt: NOW + 400 + offset }), onSubmit)); });
    expect(count(r, 'shiro')).toBe(shiro);
  });
});

describe('a stamp left by a write the match\'s leaving running dropped is cleared (bc-evts)', () => {
  it('sent back to the queue inside the debounce: a later scheduled copy stamped before the tap is adopted', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let r;
    await act(async () => { r = render(el(engi(), onSubmit)); });
    await tap(r, 'engi-aka-inc');
    await tap(r, 'engi-aka-dec'); // net zero: the board reads as untouched again
    // Another device sends the match back to the queue, same score, inside the
    // debounce: the autosave is dropped at fire time, so no landed save clears the stamp.
    await act(async () => { r.rerender(el(engi({ status: 'scheduled' }), onSubmit)); });
    await wait(350);
    expect(onSubmit, 'the autosave was dropped').not.toHaveBeenCalled();
    // A later-arriving copy, stamped before the tap (and before the match left running).
    await act(async () => { r.rerender(el(engi({ status: 'scheduled', flagsB: 1, modifiedAt: NOW - 4000 }), onSubmit)); });
    expect(count(r, 'shiro'), 'the copy is adopted, not held against a stamp nothing can clear').toBe('1');
  });
});
