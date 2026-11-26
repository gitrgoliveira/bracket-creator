// A self-run team competition still in setup has no Register button, because
// the public form takes individuals only. Its card on the viewer home says
// how teams get in instead, in the one sentence the registration page also
// uses. "Team" is decided by isTeamCompetition (a team kind or a team size),
// so a competition stored with a team size and another kind is a team one on
// the card's button, its count and its kind label alike.
import React from 'react';
import { render, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null) },
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const SENTENCE = 'Teams are entered by the tournament organizer.';

let restoreGlobals;
let ViewerHome;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ ViewerHome } = await import('../../viewer_home.jsx'));
});

afterAll(() => restoreGlobals());

const comp = (overrides) => ({
  id: 'c1', name: 'Open', kind: 'individual', teamSize: 0, format: 'knockout',
  status: 'setup', courts: ['A'], players: [{ id: 'p1', name: 'A' }], poolMatches: [],
  bracket: { rounds: [] },
  ...overrides,
});

const home = (mode, c) => (
  <ViewerHome
    tournament={{ mode, name: 'T', date: '', courts: ['A'], competitions: [c] }}
    onSelectCompetition={vi.fn()} onAdminClick={vi.fn()} onOpenSchedule={vi.fn()}
    onRegister={vi.fn()} onOpenResults={vi.fn()}
  />
);

async function mount(mode, c) {
  await act(async () => { render(home(mode, c)); });
}

const registerButton = () => screen.queryByRole('button', { name: 'Register for this competition' });

describe('self-run team competition in setup: how teams get in', () => {
  it.each([
    ['a team kind', { kind: 'team', teamSize: 3 }],
    ['a team size and another kind', { kind: 'individual', teamSize: 3 }],
  ])('says teams are entered by the organizer, with no Register button: %s', async (_n, overrides) => {
    await mount('self-run', comp(overrides));
    expect(screen.getByText(SENTENCE)).toBeTruthy();
    expect(registerButton()).toBeNull();
  });

  it('counts teams and names the kind Teams for a team size under another kind', async () => {
    await mount('self-run', comp({ kind: 'individual', teamSize: 3 }));
    const meta = document.querySelector('.vlist-item__meta');
    expect(meta.textContent).toMatch(/^Teams, 3-person, 1 teams/);
  });

  it('keeps Register and says nothing about teams for an individual competition', async () => {
    await mount('self-run', comp({}));
    expect(registerButton()).toBeTruthy();
    expect(screen.queryByText(SENTENCE)).toBeNull();
  });

  it('says nothing once the draw is made', async () => {
    await mount('self-run', comp({ kind: 'team', teamSize: 3, status: 'knockout' }));
    expect(screen.queryByText(SENTENCE)).toBeNull();
    expect(registerButton()).toBeNull();
  });

  it('says nothing in an officiated tournament, which has no public registration at all', async () => {
    await mount('officiated', comp({ kind: 'team', teamSize: 3 }));
    expect(screen.queryByText(SENTENCE)).toBeNull();
  });
});
