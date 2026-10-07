// bc-cfbd: a double tap on a button that opens a confirm leaves the confirm
// open.
//
// The trigger's click opens the dialog, Preact renders it on the next
// microtask, and the second tap of the bounce (16-44ms later) lands on the new
// dialog layer: on the backdrop or Cancel it dismissed the confirm, and on
// Confirm it confirmed it without being asked. tap_guard.jsx's swallowBounce
// now covers every pointer click in the layer for TAP_BOUNCE_MS after it
// opens. Keyboard activation (detail 0) is never swallowed.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the guard exempts
// them and these tests could never go red.

import React from 'react';
import { render, act, cleanup, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { DialogHost, confirmDialog, Modal } from '../../ui.jsx';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

// One holder per test: a dialog a test leaves open is resolved (false) by the
// next test's dialog, and must not write into that test's result.
let outcome;

function Harness({ sink }) {
  return (
    <div>
      <button type="button" data-testid="trigger" onClick={() => {
        confirmDialog({ title: 'Add Alice?', message: 'Sure?', confirmLabel: 'Go' })
          .then((r) => { sink.value = r; });
      }}>Add</button>
      <DialogHost />
    </div>
  );
}

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });
const backdrop = (container) => container.querySelector('.modal-backdrop');

beforeEach(() => { outcome = { value: 'unresolved' }; vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe('a confirm opened by a tap', () => {
  it('survives the bounce landing on the backdrop', async () => {
    const { container } = render(<Harness sink={outcome} />);
    await pointerTap(screen.getByTestId('trigger'));
    expect(backdrop(container)).not.toBeNull();
    await wait(30);
    await pointerTap(backdrop(container));
    expect(backdrop(container)).not.toBeNull();
    expect(outcome.value).toBe('unresolved');
  });

  it('survives the bounce landing on Cancel', async () => {
    const { container } = render(<Harness sink={outcome} />);
    await pointerTap(screen.getByTestId('trigger'));
    await wait(30);
    await pointerTap(screen.getByText('Cancel'));
    expect(backdrop(container)).not.toBeNull();
    expect(outcome.value).toBe('unresolved');
  });

  it('is not confirmed by the bounce landing on Confirm', async () => {
    const { container } = render(<Harness sink={outcome} />);
    await pointerTap(screen.getByTestId('trigger'));
    await wait(30);
    await pointerTap(screen.getByText('Go'));
    expect(backdrop(container)).not.toBeNull();
    expect(outcome.value).toBe('unresolved');
  });

  it('is dismissed by a deliberate tap on the backdrop after the window', async () => {
    const { container } = render(<Harness sink={outcome} />);
    await pointerTap(screen.getByTestId('trigger'));
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(backdrop(container));
    expect(backdrop(container)).toBeNull();
    expect(outcome.value).toBe(false);
  });

  it('is confirmed by a deliberate tap on Confirm after the window', async () => {
    const { container } = render(<Harness sink={outcome} />);
    await pointerTap(screen.getByTestId('trigger'));
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(screen.getByText('Go'));
    expect(backdrop(container)).toBeNull();
    expect(outcome.value).toBe(true);
  });

  it('is confirmed at once from the keyboard (detail 0 is never a bounce)', async () => {
    render(<Harness sink={outcome} />);
    await pointerTap(screen.getByTestId('trigger'));
    await wait(30);
    await keyboardClick(screen.getByText('Go'));
    expect(outcome.value).toBe(true);
  });
});

describe('a Modal opened by a tap', () => {
  it('survives the bounce on its backdrop and closes on a deliberate tap', async () => {
    const onClose = vi.fn();
    const { container } = render(<Modal title="Share" onClose={onClose}><p>body</p></Modal>);
    await wait(30);
    await pointerTap(backdrop(container));
    expect(onClose).not.toHaveBeenCalled();
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(backdrop(container));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
