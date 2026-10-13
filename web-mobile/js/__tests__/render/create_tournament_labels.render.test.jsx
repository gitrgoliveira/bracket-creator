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

  // bc-cmpw: a self-run tournament is created with the tournament password
  // alone; choosing Self-run adds no second password field.
  it('asks for no second password once Self-run is chosen', async () => {
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Self-run' })); });
    expect(screen.queryByLabelText(/destructive-ops password/i)).toBeNull();
    expect(screen.getAllByLabelText(/password/i)).toHaveLength(1);
  });

  // Operator ruling 2026-10-13: "Self run mode has no check-in." The form
  // says so, and no longer lists check-in among what self-run makes public.
  it('says self-run has no check-in and that its scoring is public', () => {
    expect(screen.getByText(/In self-run mode, scoring is public so participants don't need this\./)).toBeInTheDocument();
    expect(screen.getByText(/Self-run mode has no check-in\./)).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('scoring, check-in');
    expect(document.body.textContent).not.toContain('scoring and check-in');
    expect(document.body.textContent).not.toContain('roster changes');
  });

  it('names the admin password, never the tournament password, in the self-run description', () => {
    expect(document.body.textContent).toContain('Scoring is public, and so are starting and completing a competition.');
    expect(document.body.textContent).toContain('need the admin password');
    expect(document.body.textContent).toContain('deleting a competition asks for it again');
    expect(document.body.textContent).not.toContain('tournament password');
  });
});
