// tap_guard.jsx owns "is this pointer tap the bounce of the previous one"
// (bc-dtfn). The hook (useArmedConfirm) is pinned through the editors in
// render/finish_arm_dwell.render.test.jsx; this pins the pure helpers.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { TAP_BOUNCE_MS, isPointerTap, stampTap, clearTap, tapIsBounce, acceptTap, swallowBounce, useOpenedTapGuard } from '../tap_guard.jsx';

const pointer = { detail: 1 };
const keyboard = { detail: 0 };

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { vi.useRealTimers(); });

describe('isPointerTap', () => {
  it('reads a keyboard-synthesized click (detail 0) as not a pointer tap', () => {
    expect(isPointerTap(pointer)).toBe(true);
    expect(isPointerTap({ detail: 2 })).toBe(true);
    expect(isPointerTap(keyboard)).toBe(false);
  });
});

describe('tapIsBounce', () => {
  it('is false before any accepted tap', () => {
    expect(tapIsBounce({ current: null }, pointer)).toBe(false);
  });

  it('is true inside the window and false from its edge on', () => {
    const ref = { current: null };
    stampTap(ref);
    vi.advanceTimersByTime(TAP_BOUNCE_MS - 1);
    expect(tapIsBounce(ref, pointer)).toBe(true);
    vi.advanceTimersByTime(1);
    expect(tapIsBounce(ref, pointer)).toBe(false);
  });

  it('never swallows keyboard input', () => {
    const ref = { current: null };
    stampTap(ref);
    expect(tapIsBounce(ref, keyboard)).toBe(false);
  });

  it('scopes stamps by key, so a tap on something else is not a bounce', () => {
    const ref = { current: null };
    stampTap(ref, 'a');
    expect(tapIsBounce(ref, pointer, 'a')).toBe(true);
    expect(tapIsBounce(ref, pointer, 'b')).toBe(false);
    expect(tapIsBounce(ref, pointer)).toBe(false);
  });

  it('a wall clock stepped back is never read as a bounce', () => {
    // The device corrected its time: the gap since the stamp is negative until
    // the clock catches up with it, which used to swallow every tap meanwhile.
    const ref = { current: null };
    vi.setSystemTime(new Date('2026-09-27T12:00:00Z'));
    stampTap(ref);
    vi.setSystemTime(new Date('2026-09-27T11:59:00Z'));
    expect(tapIsBounce(ref, pointer)).toBe(false);
  });

  it('clearTap forgets the stamp', () => {
    const ref = { current: null };
    stampTap(ref, 'a');
    clearTap(ref, 'a');
    expect(tapIsBounce(ref, pointer, 'a')).toBe(false);
  });
});

describe('acceptTap', () => {
  it('accepts and stamps a tap, then refuses its bounce', () => {
    const ref = { current: null };
    expect(acceptTap(ref, pointer, 'a')).toBe(true);
    expect(acceptTap(ref, pointer, 'a')).toBe(false);
    // A refused bounce does not re-stamp: the window still runs from the tap.
    vi.advanceTimersByTime(TAP_BOUNCE_MS);
    expect(acceptTap(ref, pointer, 'a')).toBe(true);
  });

  it('accepts a keyboard click inside the window', () => {
    const ref = { current: null };
    acceptTap(ref, pointer);
    expect(acceptTap(ref, keyboard)).toBe(true);
  });
});

describe('swallowBounce', () => {
  it('stops a bounce and lets anything else through', () => {
    const ref = { current: null };
    const handler = swallowBounce(ref);
    const ev = (detail) => ({ detail, stopPropagation: vi.fn(), preventDefault: vi.fn() });

    const early = ev(1);
    handler(early);
    expect(early.stopPropagation).not.toHaveBeenCalled();

    stampTap(ref);
    const bounce = ev(1);
    handler(bounce);
    expect(bounce.stopPropagation).toHaveBeenCalled();
    expect(bounce.preventDefault).toHaveBeenCalled();

    const key = ev(0);
    handler(key);
    expect(key.stopPropagation).not.toHaveBeenCalled();

    vi.advanceTimersByTime(TAP_BOUNCE_MS);
    const late = ev(1);
    handler(late);
    expect(late.stopPropagation).not.toHaveBeenCalled();
  });
});

describe('useOpenedTapGuard', () => {
  const ev = (detail) => ({ detail, stopPropagation: vi.fn(), preventDefault: vi.fn() });

  it('stamps when a node mounts and ignores the unmount call', () => {
    const { openedRef, onClickCapture } = useOpenedTapGuard();
    openedRef(null);
    const unmounted = ev(1);
    onClickCapture(unmounted);
    expect(unmounted.stopPropagation).not.toHaveBeenCalled();

    openedRef({});
    const bounce = ev(1);
    onClickCapture(bounce);
    expect(bounce.stopPropagation).toHaveBeenCalled();
    expect(bounce.preventDefault).toHaveBeenCalled();
  });

  it('lets a pointer click through once the window has passed', () => {
    const { openedRef, onClickCapture } = useOpenedTapGuard();
    openedRef({});
    vi.advanceTimersByTime(TAP_BOUNCE_MS);
    const late = ev(1);
    onClickCapture(late);
    expect(late.stopPropagation).not.toHaveBeenCalled();
  });

  it('never swallows a keyboard click (detail 0)', () => {
    const { openedRef, onClickCapture } = useOpenedTapGuard();
    openedRef({});
    const key = ev(0);
    onClickCapture(key);
    expect(key.stopPropagation).not.toHaveBeenCalled();
  });

  it('a new mount re-stamps (a request replacing another)', () => {
    const { openedRef, onClickCapture } = useOpenedTapGuard();
    openedRef({});
    vi.advanceTimersByTime(TAP_BOUNCE_MS + 10);
    openedRef({});
    const bounce = ev(1);
    onClickCapture(bounce);
    expect(bounce.stopPropagation).toHaveBeenCalled();
  });

  it('with backdropOnly, swallows a bounce on the backdrop but not on a control inside it', () => {
    const { openedRef, onClickCapture } = useOpenedTapGuard({ backdropOnly: true });
    openedRef({});
    const backdrop = {};
    const onBackdrop = { ...ev(1), target: backdrop, currentTarget: backdrop };
    onClickCapture(onBackdrop);
    expect(onBackdrop.stopPropagation).toHaveBeenCalled();
    const onControl = { ...ev(1), target: {}, currentTarget: backdrop };
    onClickCapture(onControl);
    expect(onControl.stopPropagation).not.toHaveBeenCalled();
  });
});
