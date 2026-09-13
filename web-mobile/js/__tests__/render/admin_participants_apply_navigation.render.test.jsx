import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent } from '@testing-library/react';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-pnum operator ruling 1: "Apply returns to the dashboard: it needs to go
// to what would be the next action/view." A clean Apply (no near-duplicate
// warnings) navigates the operator to whatever comes next instead of the
// competition dashboard: a competition still in setup goes to the Overview
// checklist (it names the next step: seeds and settings, generate the
// draw); a started competition goes to Scoring, the same destination as
// this page's own "Go to Scoring" CTA. When the save surfaces near-dup
// warnings, the operator stays put to review the banner.
//
// Since bc-prow the box starts empty and an Apply over an existing roster
// asks for confirmation first, so each case types a list and accepts the
// replace dialog (admin_participants_apply_replaces.render.test.jsx pins the
// dialog itself).
//
// Mounted for REAL (not stubbed), same setup as the sibling
// admin_participants_*.render.test.jsx files. The shared harness lives in
// admin_participants_mount_harness.jsx.

installParticipantsHarness();

const ROSTER = 'Alice, Dojo Alice\nBob, Dojo Bob';

async function typeAndApply(container) {
  const box = container.querySelector('textarea');
  expect(box).toBeTruthy();
  await act(async () => { fireEvent.change(box, { target: { value: ROSTER } }); });
  const btn = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Apply changes');
  expect(btn).toBeTruthy();
  await act(async () => { fireEvent.click(btn); });
}

describe('AdminParticipants Apply navigation (bc-pnum operator ruling 1)', () => {
  let savedConfirm;

  beforeEach(() => {
    savedConfirm = window.confirmDialog;
    window.confirmDialog = vi.fn().mockResolvedValue(true);
  });

  afterEach(() => {
    window.confirmDialog = savedConfirm;
  });

  it('a clean Apply on a setup competition navigates to Overview, not the dashboard', async () => {
    const onSection = vi.fn();
    const onUpdate = vi.fn(async () => []); // no near-dup warnings
    const { container } = await mountParticipants(
      makeParticipantsCompetition({ status: 'setup' }),
      { onUpdate, onSection },
    );

    await typeAndApply(container);

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onSection).toHaveBeenCalledWith('overview');
  });

  it('a clean Apply on a started competition navigates to Scoring, not the dashboard', async () => {
    const onSection = vi.fn();
    const onUpdate = vi.fn(async () => []);
    const { container } = await mountParticipants(
      makeParticipantsCompetition({ status: 'pools' }),
      { onUpdate, onSection },
    );

    await typeAndApply(container);

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onSection).toHaveBeenCalledWith('scores');
  });

  it('an Apply that surfaces near-duplicate warnings stays on the page (no navigation)', async () => {
    const onSection = vi.fn();
    const onUpdate = vi.fn(async () => ([{ a: 'Alice', b: 'Alicia' }]));
    const { container } = await mountParticipants(
      makeParticipantsCompetition({ status: 'setup' }),
      { onUpdate, onSection },
    );

    await typeAndApply(container);

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onSection).not.toHaveBeenCalled();
  });
});
