import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { installSettingsHarness, makeSettingsCompetition } from './settings_mount_harness.jsx';

installSettingsHarness();

function renderSettings(initialComp, onUpdate, { showToast = () => {} } = {}) {
  const AdminComp = window.AdminCompetition;
  const t = {
    name: 'Spring Taikai',
    courts: ['A', 'B'],
    competitions: [initialComp, { id: 'c2', name: 'Yudansha' }],
  };

  const Wrapper = ({ comp }) => (
    <AdminComp
      tournament={t}
      competition={comp}
      pools={[]}
      poolMatches={[]}
      standings={[]}
      bracket={null}
      section="settings"
      onSection={() => {}}
      onBack={() => {}}
      onOpenCompetition={() => {}}
      onUpdate={onUpdate}
      onRefreshCompetition={() => {}}
      onMoveCourt={() => {}}
      onEditScore={() => {}}
      onLogout={() => {}}
      onViewerMode={() => {}}
      tweaks={{}}
      password=""
      showToast={showToast}
    />
  );

  let rendered;
  act(() => {
    rendered = render(<Wrapper comp={initialComp} />);
  });

  return {
    ...rendered,
    updateComp: async (newComp) => {
      await act(async () => {
        rendered.rerender(<Wrapper comp={newComp} />);
      });
    },
  };
}

describe('Settings re-time warning for draw-ready competitions (c-setretime)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    window.API.previewMove = vi.fn().mockResolvedValue({
      retime: {
        anyChange: true,
        matchCount: 31,
        notFreshCount: 0,
        courts: [{ court: 'A', oldStart: 855, oldEnd: 962, newStart: 900, newEnd: 1007 }],
      },
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it('1. draw-ready competition, Start time changed: after 250 ms previewMove called once and warning renders with exact copy and no line-through', async () => {
    const comp = makeSettingsCompetition({
      id: 'c1',
      status: 'draw-ready',
      date: '12-05-2026',
      startTime: '09:00',
      courts: ['A'],
    });

    const { container } = renderSettings(comp, vi.fn());
    const startInput = container.querySelector('#settings-start-time');

    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    expect(window.API.previewMove).not.toHaveBeenCalled();

    await act(async () => {
      vi.advanceTimersByTime(250);
    });

    expect(window.API.previewMove).toHaveBeenCalledTimes(1);
    expect(window.API.previewMove).toHaveBeenCalledWith(
      {
        competitionId: 'c1',
        date: '12-05-2026',
        startTime: '15:00',
        courts: ['A'],
      },
      '',
      expect.any(AbortSignal)
    );

    const warning = container.querySelector('[role="status"]');
    expect(warning).not.toBeNull();
    expect(warning.textContent).toContain('Saving moves its 31 matches, in the same order:');
    expect(warning.textContent).toContain('Shiaijo A: 15:00 to 16:47 (was 14:15 to 16:02)');
    expect(warning.textContent).toContain('The pools, the bracket and the competitor numbers stay as they are');

    expect(warning.querySelector('s')).toBeNull();
    expect(warning.querySelector('del')).toBeNull();
    expect(warning.innerHTML).not.toContain('line-through');
  });

  it('2. draw-ready, Date changed (not the start time): the request carries the new date and the unchanged start time; warning shows', async () => {
    const comp = makeSettingsCompetition({
      id: 'c1',
      status: 'draw-ready',
      date: '12-05-2026',
      startTime: '09:00',
      courts: ['A'],
    });

    const { container } = renderSettings(comp, vi.fn());
    const dayInput = container.querySelector('#settings-day');

    await act(async () => {
      fireEvent.change(dayInput, { target: { value: '2026-05-13' } });
    });

    await act(async () => {
      vi.advanceTimersByTime(250);
    });

    expect(window.API.previewMove).toHaveBeenCalledTimes(1);
    expect(window.API.previewMove).toHaveBeenCalledWith(
      {
        competitionId: 'c1',
        date: '13-05-2026',
        startTime: '09:00',
        courts: ['A'],
      },
      '',
      expect.any(AbortSignal)
    );

    const warning = container.querySelector('[role="status"]');
    expect(warning).not.toBeNull();
    expect(warning.textContent).toContain('Saving moves its 31 matches');
  });

  it('3. draw-ready, Start time edited but then changed back to the saved value: no warning, and in-flight request aborted', async () => {
    let capturedSignal = null;
    window.API.previewMove = vi.fn().mockImplementation((params, password, signal) => {
      capturedSignal = signal;
      return new Promise(() => {});
    });

    const comp = makeSettingsCompetition({
      id: 'c1',
      status: 'draw-ready',
      date: '12-05-2026',
      startTime: '09:00',
      courts: ['A'],
    });

    const { container } = renderSettings(comp, vi.fn());
    const startInput = container.querySelector('#settings-start-time');

    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    await act(async () => {
      vi.advanceTimersByTime(250);
    });

    expect(window.API.previewMove).toHaveBeenCalledTimes(1);
    expect(capturedSignal).not.toBeNull();
    expect(capturedSignal.aborted).toBe(false);

    await act(async () => {
      fireEvent.change(startInput, { target: { value: '09:00' } });
    });

    expect(capturedSignal.aborted).toBe(true);
    expect(container.querySelector('[role="status"]')).toBeNull();
  });

  it('4. draw-ready, another field (for example Name) edited only: previewMove is never called, no warning', async () => {
    const comp = makeSettingsCompetition({
      id: 'c1',
      status: 'draw-ready',
      date: '12-05-2026',
      startTime: '09:00',
      courts: ['A'],
    });

    const { container } = renderSettings(comp, vi.fn());
    const nameInput = container.querySelector('#settings-display-name');

    await act(async () => {
      fireEvent.change(nameInput, { target: { value: 'Summer Open' } });
    });

    await act(async () => {
      vi.advanceTimersByTime(500);
    });

    expect(window.API.previewMove).not.toHaveBeenCalled();
    expect(container.querySelector('[role="status"]')).toBeNull();
  });

  it('5. status "setup", Start time changed: previewMove never called, no warning', async () => {
    const comp = makeSettingsCompetition({
      id: 'c1',
      status: 'setup',
      date: '12-05-2026',
      startTime: '09:00',
      courts: ['A'],
    });

    const { container } = renderSettings(comp, vi.fn());
    const startInput = container.querySelector('#settings-start-time');

    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    await act(async () => {
      vi.advanceTimersByTime(500);
    });

    expect(window.API.previewMove).not.toHaveBeenCalled();
    expect(container.querySelector('[role="status"]')).toBeNull();
  });

  it('6. draw-ready with notFreshCount 3: the DR2 line is shown; with notFreshCount 0 it is absent', async () => {
    window.API.previewMove = vi.fn().mockResolvedValue({
      retime: {
        anyChange: true,
        matchCount: 31,
        notFreshCount: 3,
        courts: [{ court: 'A', oldStart: 855, oldEnd: 962, newStart: 900, newEnd: 1007 }],
      },
    });

    const comp = makeSettingsCompetition({
      id: 'c1',
      status: 'draw-ready',
      date: '12-05-2026',
      startTime: '09:00',
      courts: ['A'],
    });

    const { container, unmount } = renderSettings(comp, vi.fn());
    const startInput = container.querySelector('#settings-start-time');

    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    await act(async () => {
      vi.advanceTimersByTime(250);
    });

    const warning = container.querySelector('[role="status"]');
    expect(warning).not.toBeNull();
    expect(warning.textContent).toContain(
      '3 of its matches no longer have the times a fresh draw would give them now: a time set by hand, a pause added or changed, or another competition moved since the draw. They get new times too.'
    );

    unmount();

    window.API.previewMove = vi.fn().mockResolvedValue({
      retime: {
        anyChange: true,
        matchCount: 31,
        notFreshCount: 0,
        courts: [{ court: 'A', oldStart: 855, oldEnd: 962, newStart: 900, newEnd: 1007 }],
      },
    });

    const { container: container0 } = renderSettings(comp, vi.fn());
    const startInput0 = container0.querySelector('#settings-start-time');

    await act(async () => {
      fireEvent.change(startInput0, { target: { value: '15:00' } });
    });

    await act(async () => {
      vi.advanceTimersByTime(250);
    });

    const warning0 = container0.querySelector('[role="status"]');
    expect(warning0).not.toBeNull();
    expect(warning0.textContent).not.toContain('no longer have the times a fresh draw would give them now');
    expect(warning0.textContent).not.toContain('no longer has the time a fresh draw would give it now');
  });

  it('7. draw-ready, the dry run answers anyChange:false (and no courts changed): no re-time warning block at all', async () => {
    window.API.previewMove = vi.fn().mockResolvedValue({
      retime: {
        anyChange: false,
        matchCount: 31,
        notFreshCount: 0,
        courts: [],
      },
    });

    const comp = makeSettingsCompetition({
      id: 'c1',
      status: 'draw-ready',
      date: '12-05-2026',
      startTime: '09:00',
      courts: ['A'],
    });

    const { container } = renderSettings(comp, vi.fn());
    const startInput = container.querySelector('#settings-start-time');

    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    await act(async () => {
      vi.advanceTimersByTime(250);
    });

    expect(container.querySelector('[role="status"]')).toBeNull();
  });

  it('8. Save on a draw-ready competition with a changed start time sends PUT with pinned configVersion, no players key, and toast repeats count', async () => {
    const onUpdate = vi.fn().mockResolvedValue({
      configVersion: 'v-next',
      previousTimes: Array.from({ length: 31 }, () => ({})),
    });
    const showToast = vi.fn();

    const comp = makeSettingsCompetition({
      id: 'c1',
      status: 'draw-ready',
      configVersion: 'v-pinned-version',
      date: '12-05-2026',
      startTime: '09:00',
      courts: ['A'],
      players: [{ id: 'p1', name: 'Alice', dojo: 'Dojo A' }],
    });

    const { container } = renderSettings(comp, onUpdate, { showToast });
    const startInput = container.querySelector('#settings-start-time');

    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    await act(async () => {
      vi.advanceTimersByTime(250);
    });

    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === 'Save changes'
    );
    expect(saveBtn.disabled).toBe(false);

    await act(async () => {
      fireEvent.click(saveBtn);
    });

    expect(onUpdate).toHaveBeenCalledTimes(1);
    const putBody = onUpdate.mock.calls[0][0];
    expect(putBody.configVersion).toBe('v-pinned-version');
    expect(putBody.startTime).toBe('15:00');
    expect('players' in putBody).toBe(false);

    expect(showToast).toHaveBeenCalledWith('Competition settings saved. 31 matches have new times.');
  });

  it('9. Save rejected with retime_unavailable: sentence is on screen, warning still on screen, edit still staged', async () => {
    const sentence = "This competition's match order could not be read back, so its matches were not moved. Nothing was changed.";
    const err = new Error(sentence);
    err.code = 'retime_unavailable';

    const onUpdate = vi.fn().mockRejectedValue(err);

    const comp = makeSettingsCompetition({
      id: 'c1',
      status: 'draw-ready',
      configVersion: 'v-pinned',
      date: '12-05-2026',
      startTime: '09:00',
      courts: ['A'],
    });

    const { container } = renderSettings(comp, onUpdate);
    const startInput = container.querySelector('#settings-start-time');

    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    await act(async () => {
      vi.advanceTimersByTime(250);
    });

    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === 'Save changes'
    );
    expect(saveBtn.disabled).toBe(false);

    await act(async () => {
      fireEvent.click(saveBtn);
    });

    expect(container.textContent).toContain(sentence);
    expect(container.querySelector('[role="status"]')).not.toBeNull();
    expect(saveBtn.disabled).toBe(false);
    expect(startInput.value).toBe('15:00');
  });

  it('10. The Shiaijo pills of a draw-ready competition are disabled and note is on screen; in setup pills are enabled', async () => {
    // Draw-ready
    const drawReadyComp = makeSettingsCompetition({
      id: 'c1',
      status: 'draw-ready',
      courts: ['A'],
    });
    const { container: drawnContainer, unmount: unmountDrawn } = renderSettings(drawReadyComp, vi.fn());

    const courtsGroupDrawn = drawnContainer.querySelector('.radio-group[aria-labelledby="settings-courts-label"]');
    expect(courtsGroupDrawn).not.toBeNull();
    const pillsDrawn = courtsGroupDrawn.querySelectorAll('button');
    expect(pillsDrawn.length).toBeGreaterThan(0);
    for (const pill of pillsDrawn) {
      expect(pill.disabled).toBe(true);
    }
    expect(drawnContainer.textContent).toContain('Set by the draw. Discard the draw to change shiaijo.');

    unmountDrawn();

    // Setup
    const setupComp = makeSettingsCompetition({
      id: 'c1',
      status: 'setup',
      courts: ['A'],
    });
    const { container: setupContainer } = renderSettings(setupComp, vi.fn());

    const courtsGroupSetup = setupContainer.querySelector('.radio-group[aria-labelledby="settings-courts-label"]');
    expect(courtsGroupSetup).not.toBeNull();
    const pillsSetup = courtsGroupSetup.querySelectorAll('button');
    expect(pillsSetup.length).toBeGreaterThan(0);
    for (const pill of pillsSetup) {
      expect(pill.disabled).toBe(false);
    }
    expect(setupContainer.textContent).not.toContain('Set by the draw. Discard the draw to change shiaijo.');
  });
});
