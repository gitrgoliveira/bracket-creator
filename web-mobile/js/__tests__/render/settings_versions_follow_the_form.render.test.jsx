import React from 'react';
import { render, screen, cleanup, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, afterEach, vi } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// A tournament-settings or awards save names the version of the copy the
// operator was looking at, not the freshest copy this device holds: a copy that
// another device saved in between is then refused by the server
// (tournament_changed, awards_changed) instead of being written over.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  SponsorsManager: Stub('SponsorsManager'),
  BrandingManager: Stub('BrandingManager'),
  isNonPublicOrigin: () => false,
  API: { updateCompetitionAwards: vi.fn() },
};

let restoreGlobals;
let AdminEditTournament;
let FightingSpiritAwardsEditor;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_setup.jsx');
  await import('../../admin_competition_overview.jsx');
  AdminEditTournament = window.AdminEditTournament;
  FightingSpiritAwardsEditor = window.FightingSpiritAwardsEditor;
});

afterEach(() => { cleanup(); window.API.updateCompetitionAwards.mockReset(); });
afterAll(() => restoreGlobals());

const baseTournament = (extra) => ({
  name: 'Spring Taikai',
  date: '01-07-2026',
  venue: 'Budokan',
  courts: ['A', 'B'],
  durationDays: 1,
  mode: 'officiated',
  competitions: [],
  tournamentVersion: 'v1',
  ...extra,
});

describe('Edit Tournament save version', () => {
  it('sends the version the form was opened from after another device has saved', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const props = { onSave, onCancel: noop, onLogout: noop, onViewerMode: noop, authConfig: { mode: 'file' }, password: 'pw', showToast: noop };
    let view;
    await act(async () => { view = render(<AdminEditTournament tournament={baseTournament()} {...props} />); });

    // The other device's save reaches this one while the form is open.
    await act(async () => { view.rerender(<AdminEditTournament tournament={baseTournament({ tournamentVersion: 'v2', venue: 'Elsewhere' })} {...props} />); });

    await act(async () => { fireEvent.click(screen.getByLabelText('Show the logo on the lobby display')); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Save changes' })); });

    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0][0].tournamentVersion).toBe('v1');
  });

  it('holds Save and offers Reload after the save is refused as stale', async () => {
    const stale = Object.assign(new Error('The tournament settings changed, reload them.'), { code: 'tournament_changed' });
    const onSave = vi.fn().mockRejectedValue(stale);
    const onReload = vi.fn();
    const props = { onSave, onReload, onCancel: noop, onLogout: noop, onViewerMode: noop, authConfig: { mode: 'file' }, password: 'pw', showToast: noop };
    await act(async () => { render(<AdminEditTournament tournament={baseTournament()} {...props} />); });

    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Save changes' })); });

    expect(screen.getByTestId('tournament-conflict-alert')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save changes' }).disabled).toBe(true);
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Reload' })); });
    expect(onReload).toHaveBeenCalledTimes(1);
  });
});

describe('Fighting Spirit awards save version', () => {
  const comp = (extra) => ({
    id: 'c1',
    awardsVersion: 'a1',
    fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Yamada', recipientDojo: '' }],
    ...extra,
  });
  const editor = (c) => <FightingSpiritAwardsEditor c={c} password="pw" showToast={noop} />;

  it('keeps the old version while unsaved edits keep the old list on screen', async () => {
    window.API.updateCompetitionAwards.mockResolvedValue({ awardsVersion: 'a3' });
    let view;
    await act(async () => { view = render(editor(comp())); });
    await act(async () => { fireEvent.change(screen.getByTestId('fs-award-name-0'), { target: { value: 'Tanaka' } }); });

    // Another device saved its awards: the refetch brings a newer list and version.
    await act(async () => {
      view.rerender(editor(comp({ awardsVersion: 'a2', fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Suzuki', recipientDojo: '' }] })));
    });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });

    expect(window.API.updateCompetitionAwards).toHaveBeenCalledTimes(1);
    const [, sent, , version] = window.API.updateCompetitionAwards.mock.calls[0];
    expect(sent[0].recipientName).toBe('Tanaka');
    expect(version).toBe('a1');
  });

  it('takes the newer version along with the newer list when nothing is unsaved', async () => {
    window.API.updateCompetitionAwards.mockResolvedValue({ awardsVersion: 'a3' });
    let view;
    await act(async () => { view = render(editor(comp())); });
    await act(async () => {
      view.rerender(editor(comp({ awardsVersion: 'a2', fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Suzuki', recipientDojo: '' }] })));
    });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-add')); });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });

    const [, sent, , version] = window.API.updateCompetitionAwards.mock.calls[0];
    expect(sent[0].recipientName).toBe('Suzuki');
    expect(version).toBe('a2');
  });

  it('holds Save and reloads the stored list and version after the save is refused as stale', async () => {
    const stale = Object.assign(new Error('The awards changed, reload them.'), {
      code: 'awards_changed',
      awardsVersion: 'a2',
      fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Suzuki', recipientDojo: '' }],
    });
    window.API.updateCompetitionAwards.mockRejectedValueOnce(stale).mockResolvedValue({ awardsVersion: 'a3' });
    await act(async () => { render(editor(comp())); });
    await act(async () => { fireEvent.change(screen.getByTestId('fs-award-name-0'), { target: { value: 'Tanaka' } }); });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });

    expect(screen.getByTestId('fs-award-conflict-alert')).toBeTruthy();
    expect(screen.getByTestId('fs-award-save').disabled).toBe(true);
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Tanaka');

    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-reload')); });
    expect(screen.queryByTestId('fs-award-conflict-alert')).toBeNull();
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Suzuki');

    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-add')); });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    expect(window.API.updateCompetitionAwards).toHaveBeenCalledTimes(2);
    const [, sent, , version] = window.API.updateCompetitionAwards.mock.calls[1];
    expect(sent[0].recipientName).toBe('Suzuki');
    expect(version).toBe('a2');
  });

  it('keeps the version the last save answered when a late refetch carries an older one', async () => {
    window.API.updateCompetitionAwards.mockResolvedValueOnce({ awardsVersion: 'a2' }).mockResolvedValueOnce({ awardsVersion: 'a3' }).mockResolvedValue({ awardsVersion: 'a4' });
    let view;
    await act(async () => { view = render(editor(comp())); });
    await act(async () => { fireEvent.change(screen.getByTestId('fs-award-name-0'), { target: { value: 'Tanaka' } }); });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    await act(async () => { fireEvent.change(screen.getByTestId('fs-award-name-0'), { target: { value: 'Sato' } }); });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });

    // The refetch of the first save lands after the second save.
    await act(async () => {
      view.rerender(editor(comp({ awardsVersion: 'a2', fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Tanaka', recipientDojo: '' }] })));
    });
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Sato');

    await act(async () => { fireEvent.change(screen.getByTestId('fs-award-name-0'), { target: { value: 'Ito' } }); });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    const [, sent, , version] = window.API.updateCompetitionAwards.mock.calls[2];
    expect(sent[0].recipientName).toBe('Ito');
    expect(version).toBe('a3');
  });

  it('drops a refusal from the competition just left when another competition opens', async () => {
    const stale = Object.assign(new Error('The awards changed, reload them.'), {
      code: 'awards_changed',
      awardsVersion: 'a2',
      fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Suzuki', recipientDojo: '' }],
    });
    window.API.updateCompetitionAwards.mockRejectedValueOnce(stale);
    let view;
    await act(async () => { view = render(editor(comp())); });
    await act(async () => { fireEvent.change(screen.getByTestId('fs-award-name-0'), { target: { value: 'Tanaka' } }); });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    expect(screen.getByTestId('fs-award-conflict-alert')).toBeTruthy();

    await act(async () => {
      view.rerender(editor(comp({ id: 'c2', awardsVersion: 'b1', fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Kato', recipientDojo: '' }] })));
    });
    expect(screen.queryByTestId('fs-award-conflict-alert')).toBeNull();
    expect(screen.getByTestId('fs-award-save').disabled).toBe(false);
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Kato');
  });

  it('shows no form when the competition carries no awards version (its detail did not load)', async () => {
    await act(async () => { render(editor(comp({ awardsVersion: undefined }))); });
    expect(screen.getByTestId('fs-award-unavailable')).toBeTruthy();
    expect(screen.queryByTestId('fs-award-save')).toBeNull();
  });
});
