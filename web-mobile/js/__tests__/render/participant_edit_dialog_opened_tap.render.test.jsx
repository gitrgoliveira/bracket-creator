// bc-cfbd: the participant Edit dialog, opened by a tap on a row's pencil,
// survives the bounce of that tap. The second click of a double tap lands on
// the fresh backdrop one render later, and the backdrop's onClick is the
// dialog's dismiss, which closed an untouched dialog at once. tap_guard.jsx's
// useOpenedTapGuard swallows every pointer click in the layer for
// TAP_BOUNCE_MS after it opens; a deliberate tap after the window acts as
// before, and keyboard activation (detail 0) is never swallowed.
//
// The hook lives in AdminParticipants, which outlives the dialog, so the stamp
// has to be taken each time the dialog mounts, not once with the page.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the guard exempts
// them and these tests could never go red.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, cleanup, screen, within } from '@testing-library/react';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

installParticipantsHarness();

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); });

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });
const dialog = () => screen.queryByRole('dialog');
const cancel = () => within(screen.getByRole('dialog')).getByText('Cancel');

async function mountAndOpen() {
  const { container } = await mountParticipants(makeParticipantsCompetition());
  const open = () => pointerTap(container.querySelector('button[aria-label="Edit Alice"]'));
  await open();
  expect(dialog()).not.toBeNull();
  return open;
}

describe('the participant Edit dialog opened by a tap', () => {
  it('survives the bounce landing on its backdrop', async () => {
    await mountAndOpen();
    await wait(30);
    await pointerTap(dialog());
    expect(dialog()).not.toBeNull();
  });

  it('survives the bounce landing on a control inside it', async () => {
    await mountAndOpen();
    await wait(30);
    await pointerTap(cancel());
    expect(dialog()).not.toBeNull();
  });

  it('is dismissed by a deliberate tap on the backdrop after the window', async () => {
    await mountAndOpen();
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(dialog());
    expect(dialog()).toBeNull();
  });

  it('is dismissed at once from the keyboard (detail 0 is never a bounce)', async () => {
    await mountAndOpen();
    await wait(30);
    await keyboardClick(cancel());
    expect(dialog()).toBeNull();
  });

  it('takes a fresh window each time it opens', async () => {
    const open = await mountAndOpen();
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(cancel());
    expect(dialog()).toBeNull();

    await open();
    await wait(30);
    await pointerTap(dialog());
    expect(dialog()).not.toBeNull();
  });
});
