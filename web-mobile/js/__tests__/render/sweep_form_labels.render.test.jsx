// bc-lbla, the sweep: the operator forms outside the competition-config
// screens name their fields by their visible labels too, and a toggle row
// says which option is chosen. Representative sites: the tournament Edit
// details form, the password reset form, the branding and sponsor managers,
// and the announcement composer (which renders in two places, so its ids are
// minted rather than fixed).
import React from 'react';
import { render, screen, within, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
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
  isNonPublicOrigin: () => false,
  buildCompetition: (cfg) => ({ ...cfg }),
  addMinutes: (t) => t,
  setCachedAuthConfig: noop,
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    getNumberPrefixDefault: vi.fn().mockResolvedValue({ numberPrefix: '' }),
    fetchAnnouncements: vi.fn().mockResolvedValue([]),
  },
};

let restoreGlobals;
let AdminEditTournament;
let ResetPasswordForm;
let BrandingManager;
let SponsorsManager;
let AnnouncementComposer;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ BrandingManager } = await import('../../admin_branding.jsx'));
  ({ SponsorsManager } = await import('../../admin_sponsors.jsx'));
  ({ ResetPasswordForm } = await import('../../reset.jsx'));
  await import('../../admin_announcement.jsx');
  AnnouncementComposer = window.AnnouncementComposer;
  await import('../../admin_setup.jsx');
  AdminEditTournament = window.AdminEditTournament;
});

afterAll(() => restoreGlobals());

const tournament = {
  name: 'Spring Taikai', date: '10-08-2026', durationDays: 1, venue: 'Kendo Hall',
  courts: ['A', 'B'], competitions: [], mode: 'officiated',
  publicURL: 'https://example.test', contacts: [{ label: 'Email', value: 'a@b.test' }],
};

describe('the tournament Edit details form', () => {
  it('names every field by its visible label', async () => {
    await act(async () => {
      render(
        <AdminEditTournament tournament={tournament} onCancel={noop} onSave={noop} onLogout={noop}
          onViewerMode={noop} authConfig={{ mode: 'file', elevatedConfigured: true }} password="pw" showToast={noop} />
      );
    });
    expect(screen.getByLabelText('Name').value).toBe('Spring Taikai');
    expect(screen.getByLabelText('Start date (Day 1)').type).toBe('date');
    expect(screen.getByLabelText('Number of days').value).toBe('1');
    expect(screen.getByLabelText('Venue').value).toBe('Kendo Hall');
    expect(screen.getByLabelText('Number of Shiaijo (courts)').value).toBe('2');
    expect(screen.getByLabelText('Opening ceremony (duration)').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Lunch break (duration)').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Closing ceremony (duration)').tagName).toBe('INPUT');
    // The public block opens by default because the tournament has public data.
    expect(screen.getByLabelText('Public URL').value).toBe('https://example.test');
    expect(screen.getByLabelText('Venue address').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Map link').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Opening time').type).toBe('time');
    expect(screen.getByLabelText('Closing time').type).toBe('time');
    expect(screen.getByLabelText('Website link').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Awards').tagName).toBe('TEXTAREA');
    expect(screen.getByLabelText('Notes').tagName).toBe('TEXTAREA');
    expect(screen.getByLabelText('Contact 1 value').value).toBe('a@b.test');
    expect(screen.getByRole('button', { name: 'Remove contact 1' })).toBeInTheDocument();
    expect(screen.getByLabelText('Admin password').type).toBe('password');
    expect(screen.getByLabelText('Destructive-ops password (set)').type).toBe('password');
    expect(screen.getByLabelText('Current destructive-ops password').type).toBe('password');
  });
});

describe('the password reset form', () => {
  it('names both password fields', () => {
    render(<ResetPasswordForm authConfig={{ mode: 'file', resetEnabled: true }} onBack={noop} onSuccess={noop} />);
    expect(screen.getByLabelText('New password').type).toBe('password');
    expect(screen.getByLabelText('Confirm new password').type).toBe('password');
  });
});

describe('the branding and sponsor managers', () => {
  it('names the branding fields', async () => {
    await act(async () => {
      render(<BrandingManager tournament={tournament} password="pw" showToast={noop} onThemeChange={noop} />);
    });
    expect(screen.getByLabelText('Browser tab / window title').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Primary accent color').type).toBe('color');
    expect(screen.getByLabelText('Soft accent (background tint)').type).toBe('color');
    expect(screen.getByLabelText(/Upload logo/).type).toBe('file');
  });

  it('names the sponsor fields', () => {
    render(<SponsorsManager tournament={tournament} password="pw" showToast={noop} />);
    expect(screen.getByLabelText('Sponsor name').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Link (optional)').tagName).toBe('INPUT');
    expect(screen.getByLabelText(/^Logo/).type).toBe('file');
  });
});

describe('the announcement composer', () => {
  it('names its message box and duration choices, with distinct ids for two composers', async () => {
    await act(async () => {
      render(
        <>
          <div data-testid="one"><AnnouncementComposer password="pw" showToast={noop} /></div>
          <div data-testid="two"><AnnouncementComposer password="pw" showToast={noop} /></div>
        </>
      );
    });
    const one = within(screen.getByTestId('one'));
    const two = within(screen.getByTestId('two'));
    const a = one.getByLabelText('Message');
    const b = two.getByLabelText('Message');
    expect(a.tagName).toBe('TEXTAREA');
    expect(a.id).not.toBe(b.id);
    expect(one.getByRole('radiogroup', { name: 'Duration' })).toBeInTheDocument();
  });
});
