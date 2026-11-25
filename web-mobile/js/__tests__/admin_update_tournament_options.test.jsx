// AdminApp.updateTournament's options (bc-qrps): the Posters page saves the
// poster block through the same settings door as Edit tournament, but says
// itself how the save went. `quiet` raises no toast, the page's `onRefusal`
// claims a refusal before the Edit tournament form's view check (so the
// outbox's alert never tells a refusal the page shows), and `onSettled` hands
// the page the landed save's version. Edit tournament passes no options, and
// its path is pinned unchanged here too.
//
// AdminApp is called as a plain function under the unit suite's React stub (as
// admin.test.jsx's moveMatchCourt tests do): the element it answers for a view
// carries the handlers, so `onSave` is reached without mounting anything.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import '../admin.jsx';

const deferred = () => {
  let resolve, reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  promise.catch(() => {});
  return { promise, resolve, reject };
};

const tournament = () => ({
  name: 'Spring Taikai',
  courts: ['A', 'B'],
  competitions: [],
  tournamentVersion: 'v1',
});

let showToast;
let onUpdate;

function appFor(kind) {
  return window.AdminApp({
    tournament: tournament(),
    onUpdate,
    onLogout: vi.fn(),
    onViewerMode: vi.fn(),
    onPasswordChange: vi.fn(),
    tweaks: {},
    password: 'pw',
    view: { kind },
    setView: vi.fn(),
    showToast,
    authConfig: { mode: 'file' },
  });
}

// The element that carries the view's save: Edit tournament's form sits in a
// fragment beside its dialogs.
function saver(el) {
  if (!el || typeof el !== 'object') return null;
  if (el.props && typeof el.props.onSave === 'function') return el;
  for (const child of [].concat(el.children || [], (el.props && el.props.children) || [])) {
    const found = saver(child);
    if (found) return found;
  }
  return null;
}

// The save the outbox is handed, and its answer to come.
function queueOnce() {
  const settled = deferred();
  window.API.updateTournament.mockResolvedValueOnce({ applied: true, pending: true, settled: settled.promise });
  return settled;
}

const refusal = (code) => Object.assign(new Error('The tournament settings were changed.'), {
  code, tournament: { ...tournament(), tournamentVersion: 'v9' },
});

beforeEach(() => {
  showToast = vi.fn();
  onUpdate = vi.fn();
  window.API = {
    updateTournament: vi.fn(),
    updateTournamentPassword: vi.fn(),
    fetchTournament: vi.fn(async () => tournament()),
  };
});
afterEach(() => { vi.restoreAllMocks(); });

describe('the Posters page saves through updateTournament with its options', () => {
  it('renders the Posters page for the posters view, saving quietly', () => {
    const el = appFor('posters');
    expect(el.type && el.type.name).toBe('AdminPostersPage');
    expect(typeof el.props.onSave).toBe('function');
    expect(typeof el.props.onBack).toBe('function');
    expect(typeof el.props.onEditTournament).toBe('function');
  });

  it('a landed save raises no toast and hands the page its version', async () => {
    const settled = queueOnce();
    const onSettled = vi.fn();
    const ok = await appFor('posters').props.onSave(
      { tournamentVersion: 'v1', posters: { viewer: { heading: 'Scan me' } } },
      { onSettled, onRefusal: vi.fn(() => true) },
    );
    expect(ok).toBe(true);
    const [body, password] = window.API.updateTournament.mock.calls[0];
    expect(password).toBe('pw');
    // The whole fresh tournament with the patch on top, at the page's version.
    expect(body).toMatchObject({ name: 'Spring Taikai', tournamentVersion: 'v1', posters: { viewer: { heading: 'Scan me' } } });

    settled.resolve({ tournamentVersion: 'v2' });
    await settled.promise;
    await Promise.resolve();
    expect(onSettled).toHaveBeenCalledWith({ tournamentVersion: 'v2' });
    expect(showToast).not.toHaveBeenCalled();
    expect(onUpdate.mock.calls.at(-1)[0].tournamentVersion).toBe('v2');
  });

  it('a save kept offline raises no saved-offline toast either', async () => {
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);
    queueOnce();
    await appFor('posters').props.onSave({ tournamentVersion: 'v1', posters: {} }, { onRefusal: vi.fn(() => true) });
    expect(showToast).not.toHaveBeenCalled();
  });

  it('a tournament_changed refusal reaches the page and raises no outbox alert', async () => {
    queueOnce();
    const pageRefusal = vi.fn(() => true);
    await appFor('posters').props.onSave({ tournamentVersion: 'v1', posters: {} }, { onRefusal: pageRefusal });
    const outboxRefusal = window.API.updateTournament.mock.calls[0][2].onRefusal;
    const e = refusal('tournament_changed');
    // true: claimed, so the outbox raises no alert of its own.
    expect(outboxRefusal(e)).toBe(true);
    expect(pageRefusal).toHaveBeenCalledWith(e);
  });

  it('a 400 refusal reaches the page the same way', async () => {
    queueOnce();
    const pageRefusal = vi.fn(() => true);
    await appFor('posters').props.onSave({ tournamentVersion: 'v1', posters: {} }, { onRefusal: pageRefusal });
    const outboxRefusal = window.API.updateTournament.mock.calls[0][2].onRefusal;
    const e = new Error('The heading on the Shiaijo B poster is longer than 80 characters.');
    expect(outboxRefusal(e)).toBe(true);
    expect(pageRefusal).toHaveBeenCalledWith(e);
  });

  it('a page no longer shown leaves the refusal to the outbox alert', async () => {
    queueOnce();
    await appFor('posters').props.onSave({ tournamentVersion: 'v1', posters: {} }, { onRefusal: () => false });
    const outboxRefusal = window.API.updateTournament.mock.calls[0][2].onRefusal;
    expect(outboxRefusal(refusal('tournament_changed'))).toBe(false);
  });

  it('a refusal after the organiser moved to Edit tournament is not the form\'s own late refusal', async () => {
    queueOnce();
    // The save is made from the Posters page, but the view the refusal finds is
    // Edit tournament (AdminApp reads view.kind for its ref first, for the page
    // after), and the page, no longer shown, does not claim it.
    let reads = 0;
    const view = { get kind() { return reads++ === 0 ? 'editTournament' : 'posters'; } };
    const app = window.AdminApp({
      tournament: tournament(), onUpdate, onLogout: vi.fn(), onViewerMode: vi.fn(), onPasswordChange: vi.fn(),
      tweaks: {}, password: 'pw', view, setView: vi.fn(), showToast, authConfig: { mode: 'file' },
    });
    await app.props.onSave({ tournamentVersion: 'v1', posters: {} }, { onRefusal: () => false });
    const outboxRefusal = window.API.updateTournament.mock.calls[0][2].onRefusal;
    expect(outboxRefusal(refusal('tournament_changed'))).toBe(false);
  });

  it('a save discarded from the held writes tells the page it did not land', async () => {
    const settled = queueOnce();
    const onSettled = vi.fn();
    await appFor('posters').props.onSave({ tournamentVersion: 'v1', posters: {} }, { onSettled, onRefusal: () => true });
    settled.resolve({ discarded: true });
    await settled.promise;
    await Promise.resolve();
    expect(onSettled).toHaveBeenCalledWith(null);
    expect(showToast).not.toHaveBeenCalled();
  });
});

describe('the Edit tournament form passes no options, and its path is unchanged', () => {
  it('toasts "Tournament updated" when the save lands', async () => {
    const settled = queueOnce();
    await saver(appFor('editTournament')).props.onSave({ tournamentVersion: 'v1', name: 'Autumn Taikai' });
    settled.resolve({ tournamentVersion: 'v2' });
    await settled.promise;
    await Promise.resolve();
    expect(showToast).toHaveBeenCalledWith('Tournament updated');
  });

  it('claims a refusal while the form is open', async () => {
    queueOnce();
    await saver(appFor('editTournament')).props.onSave({ tournamentVersion: 'v1', name: 'Autumn Taikai' });
    const outboxRefusal = window.API.updateTournament.mock.calls[0][2].onRefusal;
    expect(outboxRefusal(refusal('tournament_changed'))).toBe(true);
  });

  it('toasts the saved-offline line for a save kept offline', async () => {
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);
    queueOnce();
    await saver(appFor('editTournament')).props.onSave({ tournamentVersion: 'v1', name: 'Autumn Taikai' });
    expect(showToast).toHaveBeenCalledTimes(1);
    expect(showToast.mock.calls[0][1]).toBe('success');
  });
});
