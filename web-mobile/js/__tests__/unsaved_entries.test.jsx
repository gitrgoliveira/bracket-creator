// Stage 5 review 10, minor 2: a host's leave question drops the editor's
// unsaved entries at the operator's answer, whether or not the exit then
// unmounts the editor, and only through the thunk of the editor that was
// asked about: an editor swapped in while the question was open is never
// touched, and one that went away is not run.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { confirmLeave, leaveThen } from '../unsaved_entries.jsx';

const ticks = async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); };
const thunk = (yes = true) => { const f = () => yes; f.discard = vi.fn(); return f; };

let prev;
beforeEach(() => { prev = window.confirmDialog; });
afterEach(() => { window.confirmDialog = prev; });

describe('confirmLeave', () => {
  it('answers true at once with nothing unsaved and discards nothing', () => {
    const t = thunk(false);
    window.confirmDialog = vi.fn();
    expect(confirmLeave({ current: t })).toBe(true);
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(t.discard).not.toHaveBeenCalled();
  });

  it('Discard entries runs the asked editor\'s discard; Keep entries runs none', async () => {
    const t = thunk();
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    expect(await confirmLeave({ current: t })).toBe(false);
    expect(t.discard).not.toHaveBeenCalled();
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    expect(await confirmLeave({ current: t })).toBe(true);
    expect(t.discard).toHaveBeenCalledTimes(1);
  });

  it('never discards an editor swapped in while the question was open', async () => {
    const asked = thunk();
    const ref = { current: asked };
    let answer;
    window.confirmDialog = vi.fn(() => new Promise((res) => { answer = res; }));
    const pending = confirmLeave(ref);
    const swapped = thunk();
    ref.current = swapped;
    answer(true);
    expect(await pending).toBe(true);
    expect(asked.discard).toHaveBeenCalledTimes(1);
    expect(swapped.discard).not.toHaveBeenCalled();
  });

  it('leaveThen goes only on Discard entries, once the host is still there', async () => {
    const go = vi.fn();
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    leaveThen({ current: thunk() }, go, () => false);
    await ticks();
    expect(go).not.toHaveBeenCalled();
    leaveThen({ current: thunk() }, go, () => true);
    await ticks();
    expect(go).toHaveBeenCalledTimes(1);
  });
});
