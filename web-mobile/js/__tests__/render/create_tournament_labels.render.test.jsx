// bc-lbla: the first-run "Welcome to Bracket Creator" form names every field
// by its visible label, and its Tournament type buttons are a group named by
// the label above them. Before this, no label on the form was tied to its
// input, so getByLabel found none of them and a screen reader announced
// unnamed text boxes.
//
// CreateTournament is not exported: the real App renders it when the server
// has no tournament (fetchTournament answers null), so the App is mounted with
// that answer.
import React from 'react';
import { act, screen, within, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { mountApp } from '../helpers/mount_app.js';

const STUBBED_GLOBALS = {
  API: {
    fetchTournament: vi.fn(async () => null),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => null),
    subscribeToEvents: vi.fn(() => () => {}),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;

beforeAll(async () => {
  ({ unmount } = await mountApp({ path: '/', globals: STUBBED_GLOBALS }));
});

afterAll(() => { unmount(); });

describe('the first-run tournament form', () => {
  it('names every field by its visible label', () => {
    expect(screen.getByText('Welcome to Bracket Creator')).toBeInTheDocument();
    expect(screen.getByLabelText('Tournament Name').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Date').type).toBe('date');
    expect(screen.getByLabelText('Venue').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Number of Shiaijo (courts)').type).toBe('number');
    expect(screen.getByLabelText('Admin Password').type).toBe('password');
  });

  it('names the Tournament type group by its label, its buttons saying which is chosen', () => {
    const group = screen.getByRole('group', { name: 'Tournament type' });
    expect(within(group).getByRole('button', { name: 'Officiated' }).getAttribute('aria-pressed')).toBe('true');
    expect(within(group).getByRole('button', { name: 'Self-run' }).getAttribute('aria-pressed')).toBe('false');
  });

  it('names the self-run destructive-ops password once Self-run is chosen', async () => {
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Self-run' })); });
    expect(screen.getByLabelText('Destructive-ops password (required for self-run)').type).toBe('password');
  });
});
