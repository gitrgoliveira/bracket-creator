import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { ENGI_PAIR_PROBLEM, ENGI_SOLO_PROBLEM } from '../../engi_entry.jsx';

// The Registration desk asks the one engi entry rule (engi_entry.jsx) before
// it writes, on the walk-up add and on the Edit dialog alike: a pair
// competition refuses a single name, a competition with one competitor per
// side refuses a pair, each with the rule's own sentence shown under the name
// field and nothing sent. The name field's label follows the shape, as the
// Participants page's does.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

function ModalStub({ title, children, footer }) {
  return (
    <div role="dialog" aria-label={title} className="modal-stub">
      {children}
      <div className="modal-stub__footer">{footer}</div>
    </div>
  );
}

let restoreGlobals;
let AdminRegistrationDeskPage;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({
    AdminTopbar: Stub('AdminTopbar'),
    Breadcrumbs: Stub('Breadcrumbs'),
    StatusBadge: Stub('StatusBadge'),
    Modal: ModalStub,
    buildPlayerMetadata: (dan) => (dan ? [dan] : undefined),
    pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || s + 's')}`,
    API: {},
  });
  await import('../../admin_registration_desk.jsx');
  AdminRegistrationDeskPage = window.AdminRegistrationDeskPage;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  Object.assign(window.API, {
    subscribeToEvents: vi.fn(() => () => {}),
    subscribeLocalParticipantPatch: vi.fn(() => () => {}),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
    addParticipant: vi.fn().mockResolvedValue({ id: 'new-1', name: 'x', dojo: 'y' }),
    toggleCheckIn: vi.fn().mockResolvedValue({ applied: true, pending: true, settled: new Promise(() => {}) }),
    replaceParticipant: vi.fn().mockResolvedValue({ id: 'p1', name: 'x', dojo: 'y' }),
  });
});

const PAIR = 'Akira Tanaka - Ren Fujita';
const SOLO = 'Akira Tanaka';

function tournamentFor(engiSolo, players) {
  return {
    name: 'Spring Taikai',
    competitions: [{
      id: 'kata', name: 'Kata', kind: 'individual', status: 'setup', checkInEnabled: true,
      engi: true, engiSolo, players,
    }],
  };
}

async function mount(tournament, showToast = noop) {
  let result;
  await act(async () => {
    result = render(
      <AdminRegistrationDeskPage
        tournament={tournament}
        onBack={noop}
        password="pw"
        showToast={showToast}
        onUpdate={noop}
        onLogout={noop}
        onViewerMode={noop}
      />
    );
  });
  return result;
}

async function openWalkUp(engiSolo) {
  const result = await mount(tournamentFor(engiSolo, [
    { id: 'p1', name: engiSolo ? SOLO : PAIR, dojo: 'Gyokusen', checkedIn: false },
  ]));
  fireEvent.click(result.container.querySelectorAll('.rd-rail__item')[1]);
  fireEvent.click(result.getByText('+ Add walk-up'));
  return result;
}

function fillWalkUp(result, name, dojo = 'Gyokusen') {
  const inputs = result.container.querySelectorAll('.rd-walkup input');
  fireEvent.change(inputs[0], { target: { value: name } });
  fireEvent.change(inputs[1], { target: { value: dojo } });
}

function walkUpLabel(result) {
  return result.container.querySelector('.rd-walkup .field__label').textContent;
}

async function openEdit(engiSolo, storedName) {
  const stored = storedName || (engiSolo ? SOLO : PAIR);
  const result = await mount(tournamentFor(engiSolo, [
    { id: 'p1', name: stored, dojo: 'Gyokusen', checkedIn: false },
  ]));
  fireEvent.click(result.container.querySelectorAll('.rd-rail__item')[1]);
  fireEvent.click(result.container.querySelector(`button[aria-label="Edit ${stored}"]`));
  return result;
}

function editNameInput(result) {
  return result.getByRole('dialog').querySelector('input');
}

describe('Registration desk walk-up add asks the engi entry rule', () => {
  it('labels the name field with the pair format and refuses one name in a pair competition', async () => {
    const result = await openWalkUp(false);
    expect(walkUpLabel(result)).toBe('Pair (Name 1 - Name 2)');
    fillWalkUp(result, 'Akira Tanaka');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    expect(result.getByRole('alert').textContent).toBe(ENGI_PAIR_PROBLEM);
    expect(window.API.addParticipant).not.toHaveBeenCalled();
  });

  it('refuses a half pair, judged on the trimmed name', async () => {
    const result = await openWalkUp(false);
    fillWalkUp(result, 'Alice - ');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    expect(result.getByRole('alert').textContent).toBe(ENGI_PAIR_PROBLEM);
    expect(window.API.addParticipant).not.toHaveBeenCalled();
  });

  it('adds a full pair in a pair competition', async () => {
    const result = await openWalkUp(false);
    fillWalkUp(result, 'Yuki Ito - Mei Ono');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    expect(result.queryByRole('alert')).toBeNull();
    expect(window.API.addParticipant).toHaveBeenCalledWith('kata', { name: 'Yuki Ito - Mei Ono', dojo: 'Gyokusen' }, 'pw');
  });

  it('labels the name field "Name" and refuses a pair in a competition with one competitor per side', async () => {
    const result = await openWalkUp(true);
    expect(walkUpLabel(result)).toBe('Name');
    fillWalkUp(result, 'Yuki Ito - Mei Ono');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    expect(result.getByRole('alert').textContent).toBe(ENGI_SOLO_PROBLEM);
    expect(window.API.addParticipant).not.toHaveBeenCalled();
  });

  it('adds one competitor, a hyphenated name included, with one competitor per side', async () => {
    const result = await openWalkUp(true);
    fillWalkUp(result, 'Jean-Paul Martin');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    expect(result.queryByRole('alert')).toBeNull();
    expect(window.API.addParticipant).toHaveBeenCalledWith('kata', { name: 'Jean-Paul Martin', dojo: 'Gyokusen' }, 'pw');
  });

  it('takes the sentence down once the name is typed again', async () => {
    const result = await openWalkUp(true);
    fillWalkUp(result, 'Yuki Ito - Mei Ono');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    expect(result.getByRole('alert')).toBeTruthy();
    fireEvent.change(result.container.querySelectorAll('.rd-walkup input')[0], { target: { value: 'Yuki Ito' } });
    expect(result.queryByRole('alert')).toBeNull();
  });
});

describe('Registration desk Edit asks the engi entry rule', () => {
  it('refuses one name in a pair competition and sends nothing', async () => {
    const result = await openEdit(false);
    expect(result.getByRole('dialog').querySelector('.field__label').textContent).toBe('Pair (Name 1 - Name 2)');
    fireEvent.change(editNameInput(result), { target: { value: 'Akira Tanaka' } });
    await act(async () => { fireEvent.click(result.getByText('Save changes')); });
    expect(result.getByRole('alert').textContent).toBe(ENGI_PAIR_PROBLEM);
    expect(window.API.replaceParticipant).not.toHaveBeenCalled();
  });

  it('saves a full pair in a pair competition', async () => {
    const result = await openEdit(false);
    fireEvent.change(editNameInput(result), { target: { value: 'Akira Tanaka - Mei Ono' } });
    await act(async () => { fireEvent.click(result.getByText('Save changes')); });
    expect(result.queryByRole('alert')).toBeNull();
    expect(window.API.replaceParticipant).toHaveBeenCalledTimes(1);
    expect(window.API.replaceParticipant.mock.calls[0][2]).toMatchObject({ name: 'Akira Tanaka - Mei Ono', dojo: 'Gyokusen' });
  });

  it('refuses a pair with one competitor per side and sends nothing', async () => {
    const result = await openEdit(true);
    expect(result.getByRole('dialog').querySelector('.field__label').textContent).toBe('Name');
    fireEvent.change(editNameInput(result), { target: { value: 'Akira Tanaka - Ren Fujita' } });
    await act(async () => { fireEvent.click(result.getByText('Save changes')); });
    expect(result.getByRole('alert').textContent).toBe(ENGI_SOLO_PROBLEM);
    expect(window.API.replaceParticipant).not.toHaveBeenCalled();
  });

  it('saves one name with one competitor per side', async () => {
    const result = await openEdit(true);
    fireEvent.change(editNameInput(result), { target: { value: 'Akira Tanaka-Sato' } });
    await act(async () => { fireEvent.click(result.getByText('Save changes')); });
    expect(result.queryByRole('alert')).toBeNull();
    expect(window.API.replaceParticipant).toHaveBeenCalledTimes(1);
  });
});

describe('Registration desk Edit leaves a name that is left as it is unjudged', () => {
  it('saves a changed dojo on a pair row in a competition with one competitor per side', async () => {
    const result = await openEdit(true, PAIR);
    fireEvent.change(result.getByRole('dialog').querySelectorAll('input')[1], { target: { value: 'Getsurin' } });
    await act(async () => { fireEvent.click(result.getByText('Save changes')); });
    expect(result.queryByRole('alert')).toBeNull();
    expect(window.API.replaceParticipant).toHaveBeenCalledTimes(1);
    expect(window.API.replaceParticipant.mock.calls[0][2]).toMatchObject({ name: PAIR, dojo: 'Getsurin' });
  });
});

describe('Registration desk outside engi', () => {
  it('keeps "Name" and asks no engi rule for an ordinary individual competition', async () => {
    const result = await mount({
      name: 'Spring Taikai',
      competitions: [{
        id: 'men', name: "Men's Individual", kind: 'individual', status: 'setup', checkInEnabled: true,
        engiSolo: true, // stray without engi: means nothing
        players: [{ id: 'p1', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }],
      }],
    });
    fireEvent.click(result.container.querySelectorAll('.rd-rail__item')[1]);
    fireEvent.click(result.getByText('+ Add walk-up'));
    expect(walkUpLabel(result)).toBe('Name');
    fillWalkUp(result, 'Yuki Ito - Mei Ono');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    expect(result.queryByRole('alert')).toBeNull();
    expect(window.API.addParticipant).toHaveBeenCalledTimes(1);
  });
});
