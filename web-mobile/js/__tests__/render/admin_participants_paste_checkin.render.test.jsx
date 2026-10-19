// Stage 6 review 13, m4: a roster save never writes a check-in (only the
// check-in actions do), so a pasted list neither reads a trailing checked_in
// column into the players it sends nor lets the operator believe it was
// applied: the confirmation says it was not.
import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { installParticipantsHarness } from './admin_participants_mount_harness.jsx';
import { PASTED_CHECK_IN_NOT_APPLIED } from '../../admin_participants.jsx';

installParticipantsHarness();

async function flush() {
  await act(async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); });
}

function mountHost() {
  const seen = { puts: [], toasts: [] };
  function Host() {
    const c = { id: 'c1', name: 'Autumn Cup', status: 'setup', format: 'mixed', kind: 'individual',
      poolSize: 4, poolWinners: 2, checkInEnabled: true, withZekkenName: false, numberPrefix: 'K',
      players: [], rosterVersion: 'v1' };
    const onUpdate = async (next) => {
      seen.puts.push(next);
      const answer = [];
      Object.defineProperty(answer, 'rosterVersion', { value: 'v2' });
      Object.defineProperty(answer, 'rosterPlayers', { value: next.players });
      return answer;
    };
    return (
      <window.AdminParticipants c={c} tournament={{ name: 'Spring Taikai', courts: ['A'] }}
        onUpdate={onUpdate} password="" showToast={(m, t) => seen.toasts.push({ m, t })} onSection={() => {}} />
    );
  }
  return { Host, seen };
}

async function pasteAndApply(r, text) {
  const box = r.container.querySelector('textarea');
  await act(async () => { fireEvent.change(box, { target: { value: text } }); });
  const apply = Array.from(r.container.querySelectorAll('button')).find((b) => b.textContent === 'Apply changes');
  await act(async () => { fireEvent.click(apply); });
  await flush();
}

describe('a pasted list that carries a checked_in column', () => {
  it('sends no check-in and says it was not applied', async () => {
    const { Host, seen } = mountHost();
    let r;
    await act(async () => { r = render(<Host />); });
    await pasteAndApply(r, 'Alice, D1, checked_in\nBob, D2');
    expect(seen.puts.length).toBe(1);
    const sent = seen.puts[0].players;
    expect(sent.map((p) => p.name)).toEqual(['Alice', 'Bob']);
    expect(sent.map((p) => p.dojo), 'the marker is read off the row, not into the dojo').toEqual(['D1', 'D2']);
    for (const p of sent) expect('checkedIn' in p, `${p.name} carries no check-in`).toBe(false);
    const saved = seen.toasts.find((t) => /^Saved /.test(t.m));
    expect(saved, 'the save is confirmed').toBeTruthy();
    expect(saved.m).toContain(PASTED_CHECK_IN_NOT_APPLIED);
  });

  it('says nothing about check-in for a list without the column', async () => {
    const { Host, seen } = mountHost();
    let r;
    await act(async () => { r = render(<Host />); });
    await pasteAndApply(r, 'Alice, D1\nBob, D2');
    expect(seen.puts.length).toBe(1);
    for (const p of seen.puts[0].players) expect('checkedIn' in p).toBe(false);
    const saved = seen.toasts.find((t) => /^Saved /.test(t.m));
    expect(saved.m).not.toContain(PASTED_CHECK_IN_NOT_APPLIED);
    vi.restoreAllMocks();
  });
});
