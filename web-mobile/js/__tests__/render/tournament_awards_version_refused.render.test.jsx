// bc-obox G4b: Edit Tournament and the fighting-spirit awards save through the
// outbox. Each save is answered at once ({ pending, settled }): the Edit
// Tournament form closes on that answer, the awards panel stops saving and
// keeps what was typed; the server's answer comes later. A save refused as made
// against an older copy (tournament_changed, awards_changed) then shows the
// current copy in the form that is open, with the typed values kept, or is told
// by the outbox's alert when none is. A password change stays direct.
import React from 'react';
import { render, screen, cleanup, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, afterEach, vi } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const probe = { props: null };
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  promise.catch(() => {});
  return { promise, resolve, reject };
};

// The outbox's change signal: the password change's wait re-reads the queue on it.
const unsentListeners = new Set();
const tellUnsent = () => { for (const fn of unsentListeners) fn(); };

const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: admin.jsx captures this at import; the real form is
  // loaded after it (below) for the form tests.
  AdminEditTournament: (props) => { probe.props = props; return <div data-testid="edit-tournament-probe" />; },
  AdminCompetition: Stub('AdminCompetition'),
  AdminDashboard: Stub('AdminDashboard'),
  Modal: ({ title, children, footer }) => <div data-testid="modal" aria-label={title}>{children}{footer}</div>,
  confirmDialog: vi.fn(),
  subscribeUnsentWrites: (fn) => { unsentListeners.add(fn); return () => unsentListeners.delete(fn); },
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  SponsorsManager: Stub('SponsorsManager'),
  BrandingManager: Stub('BrandingManager'),
  isNonPublicOrigin: () => false,
  API: {
    subscribeToEvents: vi.fn(() => () => {}),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
    fetchTournament: vi.fn(),
    updateTournament: vi.fn(),
    updateTournamentPassword: vi.fn(),
    updateCompetitionAwards: vi.fn(),
    queuedWritesOf: vi.fn(() => 0),
    heldWrites: vi.fn(() => []),
  },
};

let restoreGlobals;
let AdminApp;
let AdminEditTournament;
let FightingSpiritAwardsEditor;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin.jsx');
  AdminApp = window.AdminApp;
  await import('../../admin_setup.jsx');
  await import('../../admin_competition_overview.jsx');
  AdminEditTournament = window.AdminEditTournament;
  FightingSpiritAwardsEditor = window.FightingSpiritAwardsEditor;
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  window.API.updateTournament.mockReset();
  window.API.updateTournamentPassword.mockReset();
  window.API.updateCompetitionAwards.mockReset();
  window.API.fetchTournament.mockReset();
  window.API.queuedWritesOf.mockReset();
  window.API.queuedWritesOf.mockReturnValue(0);
});
afterAll(() => restoreGlobals());

const tournament = (extra) => ({
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

describe('AdminApp saves the tournament settings through the outbox', () => {
  const mount = async (view = { kind: 'editTournament' }, extra = {}) => {
    const props = {
      tournament: tournament(),
      onUpdate: vi.fn(), onLogout: vi.fn(), onViewerMode: vi.fn(), onPasswordChange: vi.fn(),
      tweaks: {}, password: 'pw', setView: vi.fn(), showToast: vi.fn(), authConfig: { mode: 'file' },
      ...extra,
    };
    let view$;
    await act(async () => { view$ = render(<AdminApp {...props} view={view} />); });
    return { ...props, rerender: (v) => act(async () => { view$.rerender(<AdminApp {...props} view={v} />); }) };
  };
  const patch = (extra) => ({ tournamentVersion: 'v1', name: 'Autumn Taikai', venue: 'Budokan', courts: ['A', 'B'], ...extra });

  it('closes on the pending answer and takes the version with the server\'s answer', async () => {
    const settled = deferred();
    window.API.updateTournament.mockResolvedValue({ applied: true, pending: true, settled: settled.promise });
    const app = await mount();

    let ok;
    await act(async () => { ok = await probe.props.onSave(patch()); });

    expect(ok).toBe(true);
    expect(app.setView).toHaveBeenCalledWith({ kind: 'dashboard' });
    const [body, password] = window.API.updateTournament.mock.calls[0];
    expect(password).toBe('pw');
    expect(body.tournamentVersion).toBe('v1');
    // The operator's settings show at once; the saved toast waits for the server.
    expect(app.onUpdate.mock.calls.at(-1)[0].name).toBe('Autumn Taikai');
    expect(app.showToast).not.toHaveBeenCalled();

    await act(async () => { settled.resolve({ tournamentVersion: 'v2' }); await settled.promise; });
    expect(app.onUpdate.mock.calls.at(-1)[0].tournamentVersion).toBe('v2');
    expect(app.showToast).toHaveBeenCalledWith('Tournament updated');
  });

  it('shows the current copy and holds the typed values when the open form is refused as stale', async () => {
    const settled = deferred();
    window.API.updateTournament.mockResolvedValue({ applied: true, pending: true, settled: settled.promise });
    const app = await mount();
    await act(async () => { await probe.props.onSave(patch()); });
    // The form is open again (the operator went back to the settings) when the refusal comes.
    await app.rerender({ kind: 'editTournament' });
    const onRefusal = window.API.updateTournament.mock.calls[0][2].onRefusal;
    const stale = Object.assign(new Error('The tournament settings changed, reload them.'), {
      code: 'tournament_changed', tournament: tournament({ name: 'Other device', tournamentVersion: 'v9' }),
    });

    let claimed;
    await act(async () => { claimed = onRefusal(stale); settled.reject(stale); await settled.promise.catch(noop); });

    expect(claimed).toBe(true);
    expect(probe.props.refusal).toMatchObject({ code: 'tournament_changed' });
    // The app takes the current copy now; the form keeps its typed values until Reload.
    const copy = app.onUpdate.mock.calls.at(-1)[0];
    expect(copy.name).toBe('Other device');
    expect(copy.tournamentVersion).toBe('v9');
    expect(app.showToast).not.toHaveBeenCalledWith('Tournament updated');
  });

  it('leaves a refusal to the outbox\'s alert when no form is open, and still takes the current copy', async () => {
    const settled = deferred();
    window.API.updateTournament.mockResolvedValue({ applied: true, pending: true, settled: settled.promise });
    const app = await mount();
    await act(async () => { await probe.props.onSave(patch()); });
    await app.rerender({ kind: 'dashboard' });
    const onRefusal = window.API.updateTournament.mock.calls[0][2].onRefusal;
    const stale = Object.assign(new Error('changed'), { code: 'tournament_changed', tournament: tournament({ name: 'Other device', tournamentVersion: 'v9' }) });

    let claimed;
    await act(async () => { claimed = onRefusal(stale); settled.reject(stale); await settled.promise.catch(noop); });

    expect(claimed).toBe(false);
    expect(app.onUpdate.mock.calls.at(-1)[0].name).toBe('Other device');
  });

  it('fetches the stored settings back after a refusal on its merits', async () => {
    const settled = deferred();
    window.API.updateTournament.mockResolvedValue({ applied: true, pending: true, settled: settled.promise });
    window.API.fetchTournament.mockResolvedValue(tournament({ name: 'Spring Taikai', tournamentVersion: 'v3' }));
    const app = await mount();
    await act(async () => { await probe.props.onSave(patch()); });
    const refused = new Error('cannot set the tournament\'s shiaijo to A');

    await act(async () => { settled.reject(refused); await settled.promise.catch(noop); });

    expect(window.API.fetchTournament).toHaveBeenCalledTimes(1);
    expect(app.onUpdate.mock.calls.at(-1)[0].name).toBe('Spring Taikai');
  });

  it('puts the stored settings back when the queued save is discarded, and says nothing was saved', async () => {
    const settled = deferred();
    window.API.updateTournament.mockResolvedValue({ applied: true, pending: true, settled: settled.promise });
    window.API.fetchTournament.mockResolvedValue(tournament({ name: 'Spring Taikai', tournamentVersion: 'v1' }));
    const app = await mount();
    await act(async () => { await probe.props.onSave(patch()); });

    await act(async () => { settled.resolve({ discarded: true }); await settled.promise; });

    expect(window.API.fetchTournament).toHaveBeenCalledTimes(1);
    const shown = app.onUpdate.mock.calls.at(-1)[0];
    expect(shown.name).toBe('Spring Taikai');
    expect(shown.tournamentVersion).toBe('v1');
    expect(app.showToast).not.toHaveBeenCalledWith('Tournament updated');
  });

  it('sends a password change direct, never through the outbox', async () => {
    window.API.updateTournamentPassword.mockResolvedValue({ tournamentVersion: 'v2' });
    const app = await mount();

    let ok;
    await act(async () => { ok = await probe.props.onSave(patch({ password: 'NEW' })); });

    expect(ok).toBe(true);
    expect(window.API.updateTournament).not.toHaveBeenCalled();
    const [body, password] = window.API.updateTournamentPassword.mock.calls[0];
    expect(body.password).toBe('NEW');
    expect(body.tournamentVersion).toBe('v1');
    expect(password).toBe('pw');
    expect(app.onPasswordChange).toHaveBeenCalledWith('NEW');
    expect(app.onUpdate.mock.calls.at(-1)[0].tournamentVersion).toBe('v2');
    expect(app.showToast).toHaveBeenCalledWith('Tournament updated');
    expect(app.setView).toHaveBeenCalledWith({ kind: 'dashboard' });
  });

  it('holds a password change back while settings saves are still queued, and sends it once they arrive', async () => {
    window.API.queuedWritesOf.mockReturnValue(1);
    window.API.updateTournamentPassword.mockResolvedValue({ tournamentVersion: 'v3' });
    const app = await mount();

    let saving;
    await act(async () => { saving = probe.props.onSave(patch({ password: 'NEW' })); });

    expect(screen.getByTestId('queued-wait-text').textContent)
      .toBe('Waiting for 1 change to be sent. The password is changed once it arrives.');
    expect(window.API.queuedWritesOf).toHaveBeenCalledWith('', ['tournament']);
    expect(window.API.updateTournamentPassword).not.toHaveBeenCalled();
    // Nothing is sent yet, so the session password is left as it was.
    expect(app.onPasswordChange).not.toHaveBeenCalled();

    window.API.queuedWritesOf.mockReturnValue(0);
    let ok;
    await act(async () => { tellUnsent(); ok = await saving; });

    expect(ok).toBe(true);
    expect(window.API.updateTournamentPassword).toHaveBeenCalledTimes(1);
    expect(app.onPasswordChange).toHaveBeenCalledWith('NEW');
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
  });

  it('sends a held password change at once when the operator chooses to', async () => {
    window.API.queuedWritesOf.mockReturnValue(1);
    window.API.updateTournamentPassword.mockResolvedValue({ tournamentVersion: 'v3' });
    await mount();

    let saving;
    await act(async () => { saving = probe.props.onSave(patch({ password: 'NEW' })); });
    let ok;
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-now')); ok = await saving; });

    expect(ok).toBe(true);
    expect(window.API.updateTournamentPassword).toHaveBeenCalledTimes(1);
  });

  it('sends nothing and keeps the form open when the operator cancels the wait', async () => {
    window.API.queuedWritesOf.mockReturnValue(1);
    const app = await mount();

    let saving;
    await act(async () => { saving = probe.props.onSave(patch({ password: 'NEW' })); });
    let ok;
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-cancel')); ok = await saving; });

    expect(ok).toBe(false);
    expect(window.API.updateTournamentPassword).not.toHaveBeenCalled();
    expect(app.onPasswordChange).not.toHaveBeenCalled();
    expect(app.setView).not.toHaveBeenCalled();
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
    // A queue that empties later does not send the cancelled change.
    window.API.queuedWritesOf.mockReturnValue(0);
    await act(async () => { tellUnsent(); });
    expect(window.API.updateTournamentPassword).not.toHaveBeenCalled();
  });
});

describe('Edit Tournament form shows a refusal that arrives after it was saved', () => {
  const props = { onSave: vi.fn(), onCancel: noop, onReload: noop, onLogout: noop, onViewerMode: noop, authConfig: { mode: 'file' }, password: 'pw', showToast: noop };

  it('holds Save and offers Reload on a stale refusal, with the typed values kept', async () => {
    let view;
    await act(async () => { view = render(<AdminEditTournament tournament={tournament()} {...props} />); });
    await act(async () => { fireEvent.change(screen.getByDisplayValue('Spring Taikai'), { target: { value: 'Typed since' } }); });

    await act(async () => {
      view.rerender(<AdminEditTournament tournament={tournament()} refusal={{ code: 'tournament_changed', message: 'changed' }} {...props} />);
    });

    expect(screen.getByTestId('tournament-conflict-alert')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save changes' }).disabled).toBe(true);
    expect(screen.getByDisplayValue('Typed since')).toBeTruthy();
  });

  it('puts any other refusal in the form\'s banner', async () => {
    let view;
    await act(async () => { view = render(<AdminEditTournament tournament={tournament()} {...props} />); });

    await act(async () => {
      view.rerender(<AdminEditTournament tournament={tournament()} refusal={{ code: undefined, message: 'tournament mode cannot be changed after creation' }} {...props} />);
    });

    expect(screen.getByRole('alert').textContent).toContain('mode cannot be changed');
  });
});

describe('Fighting-spirit awards save through the outbox', () => {
  const comp = (extra) => ({
    id: 'c1',
    awardsVersion: 'a1',
    fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Yamada', recipientDojo: '' }],
    ...extra,
  });
  let view;
  const mount = async (showToast = vi.fn()) => {
    await act(async () => { view = render(<FightingSpiritAwardsEditor c={comp()} password="pw" showToast={showToast} />); });
    await act(async () => { fireEvent.change(screen.getByTestId('fs-award-name-0'), { target: { value: 'Tanaka' } }); });
    return showToast;
  };
  const pendingAnswer = () => {
    const settled = deferred();
    window.API.updateCompetitionAwards.mockResolvedValueOnce({ applied: true, pending: true, settled: settled.promise });
    return settled;
  };

  it('keeps the typed list and says "saved" only when the server has answered', async () => {
    const settled = deferred();
    window.API.updateCompetitionAwards.mockResolvedValue({ applied: true, pending: true, settled: settled.promise });
    const showToast = await mount();

    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });

    const [, sent, , version] = window.API.updateCompetitionAwards.mock.calls[0];
    expect(sent[0].recipientName).toBe('Tanaka');
    expect(version).toBe('a1');
    expect(showToast).not.toHaveBeenCalled();
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Tanaka');

    await act(async () => { settled.resolve({ awardsVersion: 'a2' }); await settled.promise; });
    expect(showToast).toHaveBeenCalledWith('Fighting Spirit awards saved.', 'success');
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Tanaka');
  });

  it('shows the current list in the conflict banner when refused as stale, keeping the typed values', async () => {
    const settled = deferred();
    window.API.updateCompetitionAwards.mockResolvedValue({ applied: true, pending: true, settled: settled.promise });
    const showToast = await mount();
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    const onRefusal = window.API.updateCompetitionAwards.mock.calls[0][4].onRefusal;
    const stale = Object.assign(new Error('The awards changed.'), {
      code: 'awards_changed', awardsVersion: 'a9',
      fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Suzuki', recipientDojo: '' }],
    });

    let claimed;
    await act(async () => { claimed = onRefusal(stale); settled.reject(stale); await settled.promise.catch(noop); });

    expect(claimed).toBe(true);
    expect(screen.getByTestId('fs-award-conflict-alert')).toBeTruthy();
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Tanaka');
    expect(showToast).not.toHaveBeenCalled();
    // Reload takes the list stored now.
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-reload')); });
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Suzuki');
  });

  it('leaves the refusal to the outbox\'s alert once the panel is gone', async () => {
    const settled = deferred();
    window.API.updateCompetitionAwards.mockResolvedValue({ applied: true, pending: true, settled: settled.promise });
    await mount();
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    const onRefusal = window.API.updateCompetitionAwards.mock.calls[0][4].onRefusal;
    cleanup();

    expect(onRefusal(Object.assign(new Error('changed'), { code: 'awards_changed' }))).toBe(false);
  });

  it('makes the next save from the version the last one answered, even under a late refetch of an older one', async () => {
    const first = pendingAnswer();
    const showToast = await mount();
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    await act(async () => { first.resolve({ awardsVersion: 'a2' }); await first.promise; });
    // A refetch that left before the save landed still carries the older version.
    await act(async () => {
      view.rerender(<FightingSpiritAwardsEditor c={comp({ fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Yamada', recipientDojo: '' }] })} password="pw" showToast={showToast} />);
    });
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Tanaka');

    pendingAnswer();
    await act(async () => { fireEvent.change(screen.getByTestId('fs-award-name-0'), { target: { value: 'Kato' } }); });
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });

    const [, sent, , version] = window.API.updateCompetitionAwards.mock.calls[1];
    expect(sent[0].recipientName).toBe('Kato');
    expect(version).toBe('a2');
  });

  it('keeps edits typed after the save unsaved when that save lands', async () => {
    const settled = pendingAnswer();
    const showToast = await mount();
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    await act(async () => { fireEvent.change(screen.getByTestId('fs-award-name-0'), { target: { value: 'Kato' } }); });

    await act(async () => { settled.resolve({ awardsVersion: 'a2' }); await settled.promise; });

    // A refetch of the list just saved does not put Tanaka back over Kato.
    await act(async () => {
      view.rerender(<FightingSpiritAwardsEditor c={comp({ awardsVersion: 'a2', fightingSpiritAwards: [{ title: 'Fighting Spirit', recipientName: 'Tanaka', recipientDojo: '' }] })} password="pw" showToast={showToast} />);
    });
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Kato');
  });

  it('drops a refusal from the competition just left when another competition opens', async () => {
    pendingAnswer();
    const showToast = await mount();
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    const onRefusal = window.API.updateCompetitionAwards.mock.calls[0][4].onRefusal;
    await act(async () => {
      view.rerender(<FightingSpiritAwardsEditor c={comp({ id: 'c2', awardsVersion: 'b1', fightingSpiritAwards: [] })} password="pw" showToast={showToast} />);
    });

    let claimed;
    await act(async () => { claimed = onRefusal(Object.assign(new Error('changed'), { code: 'awards_changed', awardsVersion: 'a9', fightingSpiritAwards: [] })); });

    expect(claimed).toBe(false);
    expect(screen.queryByTestId('fs-award-conflict-alert')).toBeNull();
  });

  it('says nothing was saved when the queued save is discarded, and keeps the typed list and its version', async () => {
    const settled = pendingAnswer();
    const showToast = await mount();
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });

    await act(async () => { settled.resolve({ discarded: true }); await settled.promise; });

    expect(showToast).not.toHaveBeenCalled();
    expect(screen.getByTestId('fs-award-name-0').value).toBe('Tanaka');
    pendingAnswer();
    await act(async () => { fireEvent.click(screen.getByTestId('fs-award-save')); });
    expect(window.API.updateCompetitionAwards.mock.calls[1][3]).toBe('a1');
  });
});
