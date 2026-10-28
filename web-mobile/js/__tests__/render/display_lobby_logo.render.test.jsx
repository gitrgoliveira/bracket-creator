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

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  SponsorsManager: Stub('SponsorsManager'),
  BrandingManager: Stub('BrandingManager'),
  isNonPublicOrigin: () => false,
};

let restoreGlobals;
let LobbyDisplay;
let AdminEditTournament;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../bracket.jsx');
  ({ LobbyDisplay } = await import('../../display_lobby.jsx'));
  await import('../../admin_setup.jsx');
  AdminEditTournament = window.AdminEditTournament;
});

afterEach(() => cleanup());
afterAll(() => restoreGlobals());

describe('LobbyDisplay tournament logo opt-in', () => {
  it('renders the logo image when logoOnLobby and hasLogo are both true', () => {
    const tournament = {
      name: 'Spring Taikai',
      courts: ['A'],
      theme: { logoOnLobby: true, hasLogo: true },
    };
    const { container } = render(<LobbyDisplay tournament={tournament} competitions={[]} />);
    const imgs = container.querySelectorAll('img');
    expect(imgs.length).toBe(1);
    expect(imgs[0].getAttribute('alt')).toBe('Tournament logo');
    expect(imgs[0].getAttribute('src')).toContain('/api/branding/logo');
    expect(imgs[0].className).toContain('lobby__logo');
  });

  it('renders no img when logoOnLobby is true but hasLogo is false/absent', () => {
    const tournament = {
      name: 'Spring Taikai',
      courts: ['A'],
      theme: { logoOnLobby: true },
    };
    const { container } = render(<LobbyDisplay tournament={tournament} competitions={[]} />);
    expect(container.querySelectorAll('img').length).toBe(0);
  });

  it('renders no img when hasLogo is true but logoOnLobby is false/absent', () => {
    const tournament = {
      name: 'Spring Taikai',
      courts: ['A'],
      theme: { hasLogo: true, logoOnLobby: false },
    };
    const { container } = render(<LobbyDisplay tournament={tournament} competitions={[]} />);
    expect(container.querySelectorAll('img').length).toBe(0);
  });

  it('renders no img when theme is absent', () => {
    const tournament = {
      name: 'Spring Taikai',
      courts: ['A'],
    };
    const { container } = render(<LobbyDisplay tournament={tournament} competitions={[]} />);
    expect(container.querySelectorAll('img').length).toBe(0);
  });
});

describe('AdminEditTournament "Show the logo on the lobby display" checkbox', () => {
  const baseTournament = (theme) => ({
    name: 'Spring Taikai',
    date: '01-07-2026',
    venue: 'Budokan',
    courts: ['A', 'B'],
    durationDays: 1,
    mode: 'officiated',
    competitions: [],
    ...(theme !== undefined ? { theme } : {}),
  });

  it('initialises from tournament.theme.logoOnLobby, marks dirty on toggle, and sends logoOnLobby in onSave', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    let container;
    await act(async () => {
      ({ container } = render(
        <AdminEditTournament
          tournament={baseTournament(undefined)}
          onSave={onSave}
          onCancel={noop}
          onLogout={noop}
          onViewerMode={noop}
          authConfig={{ mode: 'file' }}
          password="pw"
          showToast={noop}
        />
      ));
    });

    const checkbox = screen.getByLabelText('Show the logo on the lobby display');
    expect(checkbox.type).toBe('checkbox');
    expect(checkbox.checked).toBe(false);
    expect(container.querySelector('.edit-actions__dirty')).toBeNull();

    // Tick the checkbox: dirty indicator appears and save sends logoOnLobby: true.
    await act(async () => {
      fireEvent.click(checkbox);
    });
    expect(checkbox.checked).toBe(true);
    expect(container.querySelector('.edit-actions__dirty')).not.toBeNull();

    const saveBtn = screen.getByRole('button', { name: 'Save changes' });
    await act(async () => {
      fireEvent.click(saveBtn);
    });
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0][0].theme).toEqual({ logoOnLobby: true });
  });

  it('sends logoOnLobby: false when unticked even with no colours configured', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    await act(async () => {
      render(
        <AdminEditTournament
          tournament={baseTournament({ logoOnLobby: true })}
          onSave={onSave}
          onCancel={noop}
          onLogout={noop}
          onViewerMode={noop}
          authConfig={{ mode: 'file' }}
          password="pw"
          showToast={noop}
        />
      );
    });

    const checkbox = screen.getByLabelText('Show the logo on the lobby display');
    expect(checkbox.checked).toBe(true);

    await act(async () => {
      fireEvent.click(checkbox);
    });
    expect(checkbox.checked).toBe(false);

    const saveBtn = screen.getByRole('button', { name: 'Save changes' });
    await act(async () => {
      fireEvent.click(saveBtn);
    });
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0][0].theme).toEqual({ logoOnLobby: false });
  });
});
