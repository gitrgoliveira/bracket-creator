import React from 'react';
import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { installSettingsHarness, mountSettings, makeSettingsCompetition } from './settings_mount_harness.jsx';

installSettingsHarness();

function getExpectedField(container) {
  const label = Array.from(container.querySelectorAll('label')).find(
    (l) => l.textContent.includes('Expected participants') || l.textContent.includes('Expected teams')
  );
  if (!label) return { label: null, input: null, field: null };
  const input = container.ownerDocument.getElementById(label.getAttribute('for'));
  const field = label.closest('.field');
  return { label, input, field };
}

function getSaveButton(container) {
  return Array.from(container.querySelectorAll('button')).find(
    (b) => b.textContent.trim() === 'Save changes'
  );
}

describe('Settings Expected participants / teams field (xp-settings)', () => {
  it('renders with label "Expected participants" for individual, and "Expected teams" for team competition', async () => {
    // Individual
    const indivComp = makeSettingsCompetition({ kind: 'individual', teamSize: 0 });
    const { container: indivContainer } = await mountSettings(indivComp, vi.fn());
    const indivField = getExpectedField(indivContainer);
    expect(indivField.label).not.toBeNull();
    expect(indivField.label.textContent.trim()).toBe('Expected participants');
    expect(indivField.input).not.toBeNull();
    expect(indivField.input.getAttribute('type')).toBe('number');
    expect(indivField.input.getAttribute('min')).toBe('0');
    expect(indivField.input.getAttribute('max')).toBe('1024');

    // Team
    const teamComp = makeSettingsCompetition({ kind: 'team', teamSize: 5 });
    const { container: teamContainer } = await mountSettings(teamComp, vi.fn());
    const teamField = getExpectedField(teamContainer);
    expect(teamField.label).not.toBeNull();
    expect(teamField.label.textContent.trim()).toBe('Expected teams');
  });

  it('typing 40 and saving sends a PUT whose body has expectedParticipants 40 with the pinned configVersion', async () => {
    const onUpdate = vi.fn().mockResolvedValue({ configVersion: 'v-next' });
    const comp = makeSettingsCompetition({ configVersion: 'v-pinned' });
    const { container } = await mountSettings(comp, onUpdate);
    const { input } = getExpectedField(container);

    await act(async () => {
      fireEvent.change(input, { target: { value: '40' } });
    });

    const saveBtn = getSaveButton(container);
    expect(saveBtn.disabled).toBe(false);

    await act(async () => {
      fireEvent.click(saveBtn);
    });

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onUpdate.mock.calls[0][0].expectedParticipants).toBe(40);
    expect(onUpdate.mock.calls[0][0].configVersion).toBe('v-pinned');
  });

  it('clearing the box and saving sends 0', async () => {
    const onUpdate = vi.fn().mockResolvedValue({ configVersion: 'v-next' });
    const comp = makeSettingsCompetition({ expectedParticipants: 40, configVersion: 'v-pinned' });
    const { container } = await mountSettings(comp, onUpdate);
    const { input } = getExpectedField(container);

    await act(async () => {
      fireEvent.change(input, { target: { value: '' } });
    });

    const saveBtn = getSaveButton(container);
    expect(saveBtn.disabled).toBe(false);

    await act(async () => {
      fireEvent.click(saveBtn);
    });

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onUpdate.mock.calls[0][0].expectedParticipants).toBe(0);
    expect(onUpdate.mock.calls[0][0].configVersion).toBe('v-pinned');
  });

  it('shows standing guidance hint when empty or 0', async () => {
    const comp = makeSettingsCompetition({ expectedParticipants: 0, players: [] });
    const { container } = await mountSettings(comp, vi.fn());
    const { field } = getExpectedField(container);
    expect(field.textContent).toContain(
      'How many you expect. Estimates use this number until more register. Kept with the competition.'
    );
  });

  it('reads "Estimates use 40 until more than 40 register. 0 registered so far." with an empty roster and follows roster count', async () => {
    // Empty roster
    const compEmpty = makeSettingsCompetition({ expectedParticipants: 40, players: [] });
    const { container: cEmpty } = await mountSettings(compEmpty, vi.fn());
    const { field: fieldEmpty } = getExpectedField(cEmpty);
    expect(fieldEmpty.textContent).toContain(
      'Estimates use 40 until more than 40 register. 0 registered so far.'
    );

    // 12 registered
    const players12 = Array.from({ length: 12 }, (_, i) => ({ id: `p${i}`, name: `Player ${i}` }));
    const comp12 = makeSettingsCompetition({ expectedParticipants: 40, players: players12 });
    const { container: c12 } = await mountSettings(comp12, vi.fn());
    const { field: field12 } = getExpectedField(c12);
    expect(field12.textContent).toContain(
      '12 registered, fewer than the 40 you expected. Estimates still use 40; lower this number to use the registrations.'
    );

    // 45 registered (C >= N)
    const players45 = Array.from({ length: 45 }, (_, i) => ({ id: `p${i}`, name: `Player ${i}` }));
    const comp45 = makeSettingsCompetition({ expectedParticipants: 40, players: players45 });
    const { container: c45 } = await mountSettings(comp45, vi.fn());
    const { field: field45 } = getExpectedField(c45);
    expect(field45.textContent).toContain(
      'Estimates use the 45 registered, more than the 40 you expected.'
    );
  });

  it('shows check-in wording variant when check-in applies', async () => {
    const players12 = Array.from({ length: 12 }, (_, i) => ({ id: `p${i}`, name: `Player ${i}` }));
    // 0 checked in
    const comp0 = makeSettingsCompetition({
      expectedParticipants: 40,
      checkInEnabled: true,
      players: players12,
    });
    const { container: c0 } = await mountSettings(comp0, vi.fn());
    const { field: field0 } = getExpectedField(c0);
    expect(field0.textContent).toContain(
      'Estimates use 40 until more than 40 check in. 12 registered, 0 checked in so far.'
    );

    // 5 checked in
    const players5 = players12.map((p, i) => (i < 5 ? { ...p, checkedIn: true } : p));
    const comp5 = makeSettingsCompetition({
      expectedParticipants: 40,
      checkInEnabled: true,
      players: players5,
    });
    const { container: c5 } = await mountSettings(comp5, vi.fn());
    const { field: field5 } = getExpectedField(c5);
    expect(field5.textContent).toContain(
      '5 checked in, fewer than the 40 you expected. Estimates still use 40; lower this number to use the check-ins.'
    );
  });

  it('shows after-the-draw line naming the drawn count', async () => {
    const players16 = Array.from({ length: 16 }, (_, i) => ({ id: `p${i}`, name: `Player ${i}` }));
    // Individual
    const compDraw = makeSettingsCompetition({
      status: 'draw-ready',
      expectedParticipants: 40,
      players: players16,
    });
    const { container: cDraw } = await mountSettings(compDraw, vi.fn());
    const { field: fieldDraw } = getExpectedField(cDraw);
    expect(fieldDraw.textContent).toContain(
      'The draw has been made, so estimates use the 16 participants in it.'
    );

    // Team
    const compDrawTeam = makeSettingsCompetition({
      status: 'draw-ready',
      kind: 'team',
      teamSize: 5,
      expectedParticipants: 40,
      players: players16,
    });
    const { container: cDrawTeam } = await mountSettings(compDrawTeam, vi.fn());
    const { field: fieldDrawTeam } = getExpectedField(cDrawTeam);
    expect(fieldDrawTeam.textContent).toContain(
      'The draw has been made, so estimates use the 16 teams in it.'
    );
  });

  it('is editable (not disabled) in draw-ready status', async () => {
    const compDraw = makeSettingsCompetition({ status: 'draw-ready' });
    const { container } = await mountSettings(compDraw, vi.fn());
    const { input } = getExpectedField(container);
    expect(input).not.toBeNull();
    expect(input.disabled).toBe(false);
  });

  it('blocks save and shows error on out-of-range value', async () => {
    const comp = makeSettingsCompetition();
    const { container } = await mountSettings(comp, vi.fn());
    const { input, field } = getExpectedField(container);

    await act(async () => {
      fireEvent.change(input, { target: { value: '2000' } });
    });

    expect(field.textContent).toContain('Expected participants must be a whole number from 0 to 1024.');
    const saveBtn = getSaveButton(container);
    expect(saveBtn.disabled).toBe(true);
  });
});
