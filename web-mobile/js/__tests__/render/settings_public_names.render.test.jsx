import React from 'react';
import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { installSettingsHarness, mountSettings, makeSettingsCompetition } from './settings_mount_harness.jsx';

// bc-hnam: the competition settings' "Competitor names on public pages"
// option: the picker, what a save sends, the Swiss and free-text notes, and
// the saved competition link (copy, and a printable QR only with a Public URL).

installSettingsHarness();

const pill = (container, label) => Array.from(container.querySelectorAll('.radio-pill')).find((b) => b.textContent.trim() === label);
const saveButton = (container) => Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === 'Save changes');

afterEach(() => {
  delete global.fetch;
});

describe('Settings: competitor names on public pages', () => {
  it('offers the four choices and sends the pick with the save', async () => {
    const onUpdate = vi.fn().mockResolvedValue({ configVersion: 'v2' });
    const { container } = await mountSettings(makeSettingsCompetition({ configVersion: 'v1' }), onUpdate);

    for (const label of ['Show names', 'Numbers, a link per person', 'Numbers, one competition link', 'Numbers only']) {
      expect(pill(container, label), label).toBeTruthy();
    }
    expect(pill(container, 'Show names').getAttribute('aria-pressed')).toBe('true');

    await act(async () => { fireEvent.click(pill(container, 'Numbers only')); });
    await act(async () => { fireEvent.click(saveButton(container)); });

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onUpdate.mock.calls[0][0].publicNames).toBe('hidden');
  });

  it('leaves the option out of a save that did not touch it, so the server keeps it', async () => {
    const onUpdate = vi.fn().mockResolvedValue({ configVersion: 'v2' });
    const { container } = await mountSettings(makeSettingsCompetition({ configVersion: 'v1', publicNames: 'hidden' }), onUpdate);
    const name = container.querySelector('input.input');
    await act(async () => { fireEvent.change(name, { target: { value: 'Autumn Cup 2' } }); });
    await act(async () => { fireEvent.click(saveButton(container)); });
    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect('publicNames' in onUpdate.mock.calls[0][0]).toBe(false);
    expect('revealUntil' in onUpdate.mock.calls[0][0]).toBe(false);
  });

  it('warns that free text is shown as typed, only while the competition hides names', async () => {
    const { container } = await mountSettings(makeSettingsCompetition(), vi.fn());
    expect(container.querySelector('[data-testid="public-names-free-text-warning"]')).toBeNull();
    await act(async () => { fireEvent.click(pill(container, 'Numbers only')); });
    const warn = container.querySelector('[data-testid="public-names-free-text-warning"]');
    expect(warn.textContent).toContain('shown to everyone exactly as typed');
  });

  it('tells a team competition with no kind but a team size that team names stay visible', async () => {
    // A hand-edited competition: an empty kind with a team size is still a
    // team competition (isTeamCompetition, Go's Competition.IsTeam).
    const { container } = await mountSettings(makeSettingsCompetition({ kind: '', teamSize: 3, publicNames: 'hidden' }), vi.fn());
    expect(container.textContent).toContain('Team names stay visible.');
  });

  it('asks for the expiry only while the choice has links', async () => {
    const { container } = await mountSettings(makeSettingsCompetition(), vi.fn());
    expect(container.querySelector('input[type="datetime-local"]')).toBeNull();
    await act(async () => { fireEvent.click(pill(container, 'Numbers, a link per person')); });
    const until = container.querySelector('input[type="datetime-local"]');
    expect(until).toBeTruthy();
    await act(async () => { fireEvent.change(until, { target: { value: '2026-11-22T18:00' } }); });
    await act(async () => { fireEvent.click(pill(container, 'Numbers only')); });
    expect(container.querySelector('input[type="datetime-local"]')).toBeNull();
  });

  it('says Swiss does not take the option, and offers to put names back', async () => {
    const onUpdate = vi.fn().mockResolvedValue({ configVersion: 'v2' });
    const { container } = await mountSettings(makeSettingsCompetition({ format: 'swiss', swissRounds: 4, configVersion: 'v1', publicNames: 'hidden' }), onUpdate);
    expect(container.querySelector('[data-testid="public-names-swiss"]')).toBeTruthy();
    expect(pill(container, 'Numbers only')).toBeUndefined();
    const fix = Array.from(container.querySelectorAll('button')).find((b) => b.textContent.includes('Show names for this competition'));
    await act(async () => { fireEvent.click(fix); });
    await act(async () => { fireEvent.click(saveButton(container)); });
    expect(onUpdate.mock.calls[0][0].publicNames).toBe('');
  });

  it('shows the saved competition link: Copy link, and no QR code without a Public URL', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ publicNames: 'competition', revealUntil: '', expiresAt: '2026-11-23T01:00', open: true, competitionKey: 'abc123', competitors: [] }),
    });
    const { container } = await mountSettings(makeSettingsCompetition({ publicNames: 'competition' }), vi.fn());
    await act(async () => { await Promise.resolve(); });

    const panel = container.querySelector('[data-testid="reveal-links-panel"]');
    expect(panel).toBeTruthy();
    expect(global.fetch.mock.calls[0][0]).toBe('/api/competitions/c1/reveal-links');
    const buttons = Array.from(panel.querySelectorAll('button'));
    expect(buttons.find((b) => b.textContent === 'Copy link')).toBeTruthy();
    expect(buttons.find((b) => b.textContent === 'Show QR code to print').disabled).toBe(true);
    const warning = panel.querySelector('[data-testid="reveal-links-no-public-url"]');
    expect(warning).toBeTruthy();
    expect(warning.textContent).toContain('a copied link');
    expect(panel.textContent).toContain('2026-11-23 at 01:00');
  });

  it('keeps the link back until a changed choice is saved', async () => {
    global.fetch = vi.fn();
    const { container } = await mountSettings(makeSettingsCompetition({ publicNames: 'competition' }), vi.fn());
    global.fetch.mockClear();
    await act(async () => { fireEvent.click(pill(container, 'Numbers only')); });
    expect(container.querySelector('[data-testid="reveal-links-panel"]')).toBeNull();
    await act(async () => { fireEvent.click(pill(container, 'Numbers, one competition link')); });
    // Back to what is saved: nothing waits for a save, and the link is read again.
    expect(container.querySelector('[data-testid="reveal-links-unsaved"]')).toBeNull();
    expect(container.querySelector('[data-testid="reveal-links-panel"]')).toBeTruthy();
    expect(global.fetch).toHaveBeenCalled();
  });
});
