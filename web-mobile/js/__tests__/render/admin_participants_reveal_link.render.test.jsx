import React from 'react';
import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-hnam: with a link per person, every participant row offers "Reveal link".
// A tap reads that one competitor's key (the server logs each read) and shows
// the link as a QR code on this screen; nothing is read before the tap, and
// with any other choice the row offers no button.

installParticipantsHarness();
beforeAll(async () => {
  // qr.jsx publishes window.qrFits / window.renderQR, which the sheet reads.
  await import('../../qr.jsx');
});

afterEach(() => {
  delete global.fetch;
  vi.restoreAllMocks();
});

const revealButtons = (container) => Array.from(container.querySelectorAll('button.seed-row__reveal'));
const answer = (body) => ({ ok: true, json: async () => body });

describe('Participants: Reveal link', () => {
  it('reads nothing until a tap, then shows that competitor\'s own link as a QR code', async () => {
    global.fetch = vi.fn().mockResolvedValue(answer({ id: 'p-2', name: 'Bob', key: 'key-bob', open: true, expiresAt: '2099-01-01T10:00' }));
    const { container } = await mountParticipants(makeParticipantsCompetition({ publicNames: 'person' }));
    await act(async () => { await Promise.resolve(); });

    expect(global.fetch).not.toHaveBeenCalled();
    const buttons = revealButtons(container);
    expect(buttons.map((b) => b.getAttribute('aria-label'))).toEqual(['Reveal link for Alice', 'Reveal link for Bob']);
    expect(buttons[0].textContent).toBe('Reveal link');

    await act(async () => { fireEvent.click(buttons[1]); });
    await act(async () => { await Promise.resolve(); });
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(global.fetch.mock.calls[0][0]).toBe('/api/competitions/c1/reveal-links/p-2');
    expect(document.querySelector('[data-testid="share-link-url"]').textContent).toBe(`${window.location.origin}/reveal/c1#key-bob`);
    expect(document.querySelector('canvas.share-link__qr')).not.toBeNull();
    expect(document.body.textContent).toContain('Reveal link: Bob');
    expect(document.body.textContent).toContain('Turn the screen so Bob can scan this code.');
    expect(document.body.textContent).toContain('It works until 2099-01-01 at 10:00.');
    expect(document.querySelector('[data-testid="reveal-competitor-no-public-url"]')).not.toBeNull();
  });

  it('uses the Public URL, and says nothing about the address, when the tournament has one', async () => {
    global.fetch = vi.fn().mockResolvedValue(answer({ id: 'p-1', name: 'Alice', key: 'key-alice', open: true, expiresAt: '' }));
    const { container } = await mountParticipants(makeParticipantsCompetition({ publicNames: 'person' }), { tournament: { publicURL: 'https://cup.example.org' } });
    await act(async () => { fireEvent.click(revealButtons(container)[0]); });
    await act(async () => { await Promise.resolve(); });
    expect(document.querySelector('[data-testid="share-link-url"]').textContent).toBe('https://cup.example.org/reveal/c1#key-alice');
    expect(document.querySelector('[data-testid="reveal-competitor-no-public-url"]')).toBeNull();
  });

  it('says so when the link has stopped working', async () => {
    global.fetch = vi.fn().mockResolvedValue(answer({ id: 'p-1', name: 'Alice', key: 'key-alice', open: false, expiresAt: '2026-06-02T01:00' }));
    const { container } = await mountParticipants(makeParticipantsCompetition({ publicNames: 'person' }));
    await act(async () => { fireEvent.click(revealButtons(container)[0]); });
    await act(async () => { await Promise.resolve(); });
    expect(document.querySelector('[data-testid="reveal-competitor-closed"]').textContent).toContain('The link stopped working at 2026-06-02 at 01:00.');
  });

  it('tells the organiser when the link cannot be read, and shows no sheet', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 404, json: async () => ({ error: 'this competition has no reveal link for that competitor' }) });
    const showToast = vi.fn();
    const { container } = await mountParticipants(makeParticipantsCompetition({ publicNames: 'person' }), { showToast });
    await act(async () => { fireEvent.click(revealButtons(container)[0]); });
    await act(async () => { await Promise.resolve(); });
    expect(showToast).toHaveBeenCalledWith('this competition has no reveal link for that competitor', 'error');
    expect(document.querySelector('[data-testid="share-link-url"]')).toBeNull();
  });

  it('offers no button to a competitor with no id, nor for any other choice', async () => {
    global.fetch = vi.fn();
    const noId = await mountParticipants(makeParticipantsCompetition({ publicNames: 'person', players: [{ id: 'p-1', name: 'Alice', dojo: 'Dojo Alice' }, { name: 'Bob', dojo: 'Dojo Bob' }] }));
    expect(revealButtons(noId.container).map((b) => b.getAttribute('aria-label'))).toEqual(['Reveal link for Alice']);
    noId.unmount();
    for (const publicNames of ['', 'competition', 'hidden']) {
      const { container, unmount } = await mountParticipants(makeParticipantsCompetition({ publicNames }));
      expect(revealButtons(container)).toHaveLength(0);
      unmount();
    }
    expect(global.fetch).not.toHaveBeenCalled();
  });
});
