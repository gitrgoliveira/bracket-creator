import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// bc-hnam: the tournament default for competitor names on the public pages
// (Edit tournament details) and the same option on the Add competition form.
// Mounted with real React, the window deps stubbed as admin_create_competition
// does.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const confirmDialog = vi.fn();

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  BrandingManager: Stub('BrandingManager'),
  SponsorsManager: Stub('SponsorsManager'),
  isNonPublicOrigin: () => false,
  buildCompetition: (cfg) => ({ ...cfg }),
  addMinutes: (t) => t,
  confirmDialog,
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    estimateDraft: vi.fn().mockResolvedValue(null),
    fetchCalendar: vi.fn().mockResolvedValue({ days: [], courts: [], competitions: [] }),
    getNumberPrefixDefault: vi.fn().mockResolvedValue({ numberPrefix: '' }),
  },
};

let restoreGlobals;
let AdminCreateCompetition;
let AdminEditTournament;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_setup.jsx');
  AdminCreateCompetition = window.AdminCreateCompetition;
  AdminEditTournament = window.AdminEditTournament;
});

afterAll(() => restoreGlobals());

const makeTournament = (overrides = {}) => ({
  name: 'Spring Taikai',
  venue: 'Kendo Hall',
  date: '10-08-2026',
  durationDays: 1,
  courts: ['A', 'B'],
  mode: 'officiated',
  competitions: [],
  ...overrides,
});

const pill = (container, label) => Array.from(container.querySelectorAll('button.radio-pill')).find((b) => b.textContent.trim() === label);
const buttonByText = (container, text) => Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === text);

async function mountEditor(tournament, onSave) {
  let result;
  await act(async () => {
    result = render(
      <AdminEditTournament
        tournament={tournament}
        onCancel={noop}
        onSave={onSave}
        onLogout={noop}
        onViewerMode={noop}
        authConfig={{ mode: 'file' }}
        password="pw"
        showToast={noop}
      />
    );
  });
  return result;
}

async function mountCreate(tournament, onCreate = vi.fn().mockResolvedValue(undefined)) {
  let result;
  await act(async () => {
    result = render(
      <AdminCreateCompetition tournament={tournament} onCancel={noop} onCreate={onCreate} onLogout={noop} onViewerMode={noop} password="" />
    );
  });
  return { ...result, onCreate };
}

describe('Tournament default for competitor names', () => {
  it('always sends the default, and sends no confirmation when it did not change', async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    const { container } = await mountEditor(makeTournament({ defaultPublicNames: 'person' }), onSave);
    expect(pill(container, 'Numbers, a link per person').getAttribute('aria-pressed')).toBe('true');
    await act(async () => { fireEvent.click(buttonByText(container, 'Save changes')); });
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0][0].defaultPublicNames).toBe('person');
    expect('confirmPublicNames' in onSave.mock.calls[0][0]).toBe(false);
    expect(confirmDialog).not.toHaveBeenCalled();
  });

  it('lists the competitions a change will set, and sends the yes with the save', async () => {
    confirmDialog.mockReset().mockResolvedValue(true);
    const onSave = vi.fn().mockResolvedValue(true);
    const tournament = makeTournament({
      competitions: [
        { id: 'a', name: 'Men', format: 'knockout', publicNames: '' },
        { id: 'b', name: 'Juniors', format: 'mixed', publicNames: 'hidden' },
        { id: 'c', name: 'Swiss Open', format: 'swiss', publicNames: '' },
        { id: 'd', name: 'Women', format: 'knockout', publicNames: 'hidden' },
      ],
    });
    const { container } = await mountEditor(tournament, onSave);
    await act(async () => { fireEvent.click(pill(container, 'Numbers only')); });
    await act(async () => { fireEvent.click(buttonByText(container, 'Save changes')); });

    expect(confirmDialog).toHaveBeenCalledTimes(1);
    const message = confirmDialog.mock.calls[0][0].message;
    expect(message).toContain('Men (now: Show names)');
    expect(message).not.toContain('Juniors');
    expect(message).not.toContain('Swiss Open');
    expect(message).not.toContain('Women');
    expect(onSave.mock.calls[0][0].defaultPublicNames).toBe('hidden');
    expect(onSave.mock.calls[0][0].confirmPublicNames).toBe(true);
  });

  it('sends nothing when the operator declines the list', async () => {
    confirmDialog.mockReset().mockResolvedValue(false);
    const onSave = vi.fn().mockResolvedValue(true);
    const tournament = makeTournament({ competitions: [{ id: 'a', name: 'Men', format: 'knockout', publicNames: '' }] });
    const { container } = await mountEditor(tournament, onSave);
    await act(async () => { fireEvent.click(pill(container, 'Numbers only')); });
    await act(async () => { fireEvent.click(buttonByText(container, 'Save changes')); });
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    expect(onSave).not.toHaveBeenCalled();
  });

  it('shows no public names setting in a self-run tournament', async () => {
    const { container } = await mountEditor(makeTournament({ mode: 'self-run' }), vi.fn());
    expect(pill(container, 'Numbers only')).toBeUndefined();
    expect(container.textContent).not.toContain('Competitor names on public pages');
  });

  it('warns on the awards note that free text is shown as typed, while a competition hides names', async () => {
    const hiding = makeTournament({ competitions: [{ id: 'a', name: 'Men', format: 'knockout', publicNames: 'person' }] });
    const first = await mountEditor(hiding, vi.fn());
    // The Public information section opens by default only with data in it.
    await act(async () => { fireEvent.click(first.container.querySelector('button.disclosure')); });
    expect(first.container.querySelector('[data-testid="awards-free-text-warning"]')).toBeTruthy();
    first.unmount();

    const showing = await mountEditor(makeTournament(), vi.fn());
    await act(async () => { fireEvent.click(Array.from(showing.container.querySelectorAll('button.disclosure'))[0]); });
    expect(showing.container.querySelector('[data-testid="awards-free-text-warning"]')).toBeNull();
  });
});

describe('Add competition: competitor names on public pages', () => {
  const createButton = (container) => Array.from(container.querySelectorAll('button')).find((b) => b.textContent.includes('Create & continue'));

  it('starts on the tournament default and sends the choice, expiry included', async () => {
    const { container, onCreate } = await mountCreate(makeTournament({ defaultPublicNames: 'competition' }));
    expect(pill(container, 'Numbers, one competition link').getAttribute('aria-pressed')).toBe('true');
    const until = container.querySelector('input[type="datetime-local"]');
    await act(async () => { fireEvent.change(until, { target: { value: '2026-08-10T18:00' } }); });
    await act(async () => { fireEvent.change(container.querySelector('input.input'), { target: { value: 'Men' } }); });
    await act(async () => { fireEvent.click(createButton(container)); });
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCreate.mock.calls[0][0].publicNames).toBe('competition');
    expect(onCreate.mock.calls[0][0].revealUntil).toBe('2026-08-10T18:00');
  });

  it('sends the choice "show names" explicitly, so the server never fills in the default behind it', async () => {
    const { container, onCreate } = await mountCreate(makeTournament({ defaultPublicNames: 'hidden' }));
    await act(async () => { fireEvent.click(pill(container, 'Show names')); });
    await act(async () => { fireEvent.change(container.querySelector('input.input'), { target: { value: 'Men' } }); });
    await act(async () => { fireEvent.click(createButton(container)); });
    expect(onCreate.mock.calls[0][0].publicNames).toBe('');
    expect('revealUntil' in onCreate.mock.calls[0][0]).toBe(false);
  });

  it('hides the picker for Swiss with a note, and sends names shown', async () => {
    const { container, onCreate } = await mountCreate(makeTournament({ defaultPublicNames: 'hidden' }));
    await act(async () => { fireEvent.click(pill(container, 'Swiss')); });
    expect(pill(container, 'Numbers only')).toBeUndefined();
    expect(container.querySelector('[data-testid="public-names-swiss"]')).toBeTruthy();
    await act(async () => { fireEvent.change(container.querySelector('input.input'), { target: { value: 'Open' } }); });
    await act(async () => { fireEvent.click(createButton(container)); });
    expect(onCreate.mock.calls[0][0].publicNames).toBe('');
  });

  it('shows no public names setting in a self-run tournament', async () => {
    const { container, onCreate } = await mountCreate(makeTournament({ mode: 'self-run' }));
    expect(pill(container, 'Numbers only')).toBeUndefined();
    expect(container.textContent).not.toContain('Competitor names on public pages');
    await act(async () => { fireEvent.change(container.querySelector('input.input'), { target: { value: 'Men' } }); });
    await act(async () => { fireEvent.click(createButton(container)); });
    expect(onCreate.mock.calls[0][0].publicNames).toBe('');
  });
});
