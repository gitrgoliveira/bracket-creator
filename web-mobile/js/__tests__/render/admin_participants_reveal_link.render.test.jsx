import React from 'react';
import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-hnam: with a link per person, every participant row offers "Copy reveal
// link"; with any other choice the page asks the server for nothing.

installParticipantsHarness();

afterEach(() => {
  delete global.fetch;
  vi.restoreAllMocks();
});

const revealButtons = (container) => Array.from(container.querySelectorAll('button.seed-row__reveal'));

describe('Participants: Copy reveal link', () => {
  it('puts each competitor\'s own link on the clipboard', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        publicNames: 'person', open: true,
        competitors: [{ id: 'p-1', name: 'Alice', dojo: 'Dojo Alice', key: 'key-alice' }, { id: 'p-2', name: 'Bob', dojo: 'Dojo Bob', key: 'key-bob' }],
      }),
    });
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    const showToast = vi.fn();

    const { container } = await mountParticipants(makeParticipantsCompetition({ publicNames: 'person' }), { showToast });
    await act(async () => { await Promise.resolve(); });

    expect(global.fetch.mock.calls[0][0]).toBe('/api/competitions/c1/reveal-links');
    const buttons = revealButtons(container);
    expect(buttons).toHaveLength(2);
    expect(buttons[0].getAttribute('aria-label')).toBe('Copy reveal link for Alice');

    await act(async () => { fireEvent.click(buttons[1]); });
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/reveal/c1#key-bob`);
    expect(showToast).toHaveBeenCalledWith(expect.stringContaining('Reveal link for Bob copied. It points at this device'));
  });

  it('says nothing about the address when the tournament has a Public URL', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ publicNames: 'person', open: true, competitors: [{ id: 'p-1', name: 'Alice', dojo: 'Dojo Alice', key: 'key-alice' }] }),
    });
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    const showToast = vi.fn();

    const { container } = await mountParticipants(makeParticipantsCompetition({ publicNames: 'person' }), { showToast, tournament: { publicURL: 'https://cup.example.org' } });
    await act(async () => { await Promise.resolve(); });
    await act(async () => { fireEvent.click(revealButtons(container)[0]); });
    expect(writeText).toHaveBeenCalledWith('https://cup.example.org/reveal/c1#key-alice');
    expect(showToast).toHaveBeenCalledWith('Reveal link for Alice copied');
  });

  it('reads the keys again when a swapped row keeps the roster the same size', async () => {
    const key = { 'p-1': 'key-alice', 'p-2': 'key-bob', 'p-3': 'key-cara' };
    global.fetch = vi.fn().mockImplementation(async () => ({
      ok: true,
      json: async () => ({ publicNames: 'person', open: true, competitors: Object.keys(key).map((id) => ({ id, key: key[id] })) }),
    }));
    const c = makeParticipantsCompetition({ publicNames: 'person' });
    const { container, rerender } = await mountParticipants(c);
    await act(async () => { await Promise.resolve(); });
    expect(global.fetch).toHaveBeenCalledTimes(1);

    const swapped = { ...c, players: [c.players[0], { id: 'p-3', name: 'Cara', dojo: 'Dojo Cara' }] };
    await act(async () => {
      rerender(<window.AdminParticipants c={swapped} tournament={{ name: 'Spring Taikai', courts: ['A'] }} onUpdate={() => {}} password="" showToast={() => {}} onSection={() => {}} />);
    });
    await act(async () => { await Promise.resolve(); });

    expect(global.fetch).toHaveBeenCalledTimes(2);
    expect(revealButtons(container).map((b) => b.getAttribute('aria-label'))).toEqual(['Copy reveal link for Alice', 'Copy reveal link for Cara']);
  });

  it('offers no button, and reads nothing, for any other choice', async () => {
    global.fetch = vi.fn();
    for (const publicNames of ['', 'competition', 'hidden']) {
      const { container, unmount } = await mountParticipants(makeParticipantsCompetition({ publicNames }));
      expect(revealButtons(container)).toHaveLength(0);
      unmount();
    }
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it('leaves the button out when the keys cannot be read', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({ error: 'boom' }) });
    const { container } = await mountParticipants(makeParticipantsCompetition({ publicNames: 'person' }));
    await act(async () => { await Promise.resolve(); });
    expect(revealButtons(container)).toHaveLength(0);
  });
});
