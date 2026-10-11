// bc-lbla: every competition-config control is reachable by its visible label,
// and every option pill says whether it is the chosen one.
//
// Before this, competition_fields.jsx's FieldLabel tied no label to its input,
// PillGroup's label named no control, and the pills carried their state only
// as the is-active class, so a screen reader announced a nameless text box and
// a row of plain buttons, and getByLabel found none of them. The create form's
// hand-written fields (Display name, Day, Start time, Knockout qualifiers,
// Assigned shiaijo) copied the same pattern.
//
// The shared components are rendered directly; the create form is mounted
// with real React, its window.* deps stubbed before the import (the same
// harness as admin_create_competition.render.test.jsx).
import React from 'react';
import { render, screen, within, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { PillGroup, PillButton, NumberField, TextField } from '../../competition_fields.jsx';
import {
  LABEL_TEAM_MATCH_TYPE, TEAM_MATCH_TYPE_OPTIONS, teamMatchTypeActive,
} from '../../competition_shape.jsx';

const noop = () => {};

const pressedNames = (group) => within(group).getAllByRole('button')
  .filter((b) => b.getAttribute('aria-pressed') === 'true')
  .map((b) => b.textContent.trim());

describe('the shared competition fields name their controls', () => {
  it('NumberField: the label names the number input', () => {
    render(<NumberField label="Players per pool" value={3} onChange={noop} />);
    const input = screen.getByLabelText('Players per pool');
    expect(input.tagName).toBe('INPUT');
    expect(input.type).toBe('number');
    expect(input.value).toBe('3');
  });

  it('NumberField: an optional field is named by its label and the suffix', () => {
    render(<NumberField label="Team size" optional value={5} onChange={noop} />);
    expect(screen.getByLabelText('Team size (optional)').value).toBe('5');
  });

  it('TextField: the label names the text input', () => {
    render(<TextField label="Player number prefix" value="K" onChange={noop} />);
    const input = screen.getByLabelText('Player number prefix');
    expect(input.tagName).toBe('INPUT');
    expect(input.value).toBe('K');
  });

  it('two fields on one page get two different ids', () => {
    render(
      <>
        <NumberField label="Players per pool" value={3} onChange={noop} />
        <NumberField label="Winners per pool" value={2} onChange={noop} />
      </>
    );
    const a = screen.getByLabelText('Players per pool');
    const b = screen.getByLabelText('Winners per pool');
    expect(a.id).not.toBe('');
    expect(a.id).not.toBe(b.id);
  });

  it('PillGroup: the label names the row and only the active pill is pressed', () => {
    render(
      <PillGroup
        label="Format"
        options={[{ value: 'knockout', label: 'Knockout only' }, { value: 'mixed', label: 'Pools + Knockout' }]}
        value="mixed"
        onChange={noop}
      />
    );
    const group = screen.getByRole('group', { name: 'Format' });
    const pills = within(group).getAllByRole('button');
    expect(pills).toHaveLength(2);
    // Every pill states its state: the inactive one says false, it does not
    // just leave the attribute off.
    expect(pills.map((b) => b.getAttribute('aria-pressed'))).toEqual(['false', 'true']);
    expect(pressedNames(group)).toEqual(['Pools + Knockout']);
  });

  it('PillGroup: aria-pressed reads the isActive predicate, so a stored "" announces Regular', () => {
    render(
      <PillGroup
        label={LABEL_TEAM_MATCH_TYPE}
        options={TEAM_MATCH_TYPE_OPTIONS}
        isActive={(v) => teamMatchTypeActive(v, '')}
        onChange={noop}
      />
    );
    const group = screen.getByRole('group', { name: LABEL_TEAM_MATCH_TYPE });
    expect(pressedNames(group)).toEqual(['Regular']);
    // The class and the announced state agree.
    const regular = within(group).getByRole('button', { name: 'Regular' });
    expect(regular.className).toContain('is-active');
  });

  it('PillGroup: a click still reports the option value', () => {
    const onChange = vi.fn();
    render(
      <PillGroup label="Format" options={[{ value: 'league', label: 'League' }]} value="" onChange={onChange} />
    );
    fireEvent.click(screen.getByRole('button', { name: 'League' }));
    expect(onChange).toHaveBeenCalledWith('league');
  });

  it('PillButton: passes extra props through and keeps the pill markup', () => {
    render(<PillButton active={false} onClick={noop} data-testid="orphan-court-D" style={{ color: 'red' }}>Shiaijo (court) D</PillButton>);
    const pill = screen.getByTestId('orphan-court-D');
    expect(pill.getAttribute('type')).toBe('button');
    expect(pill.className).toBe('radio-pill ');
    expect(pill.getAttribute('aria-pressed')).toBe('false');
    expect(pill.style.color).toBe('red');
  });
});

// ---------------------------------------------------------------------------
// The create-competition form's hand-written fields
// ---------------------------------------------------------------------------

const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  BrandingManager: Stub('BrandingManager'),
  SponsorsManager: Stub('SponsorsManager'),
  isNonPublicOrigin: () => false,
  buildCompetition: (cfg) => ({ ...cfg }),
  addMinutes: (t) => t,
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    getNumberPrefixDefault: vi.fn().mockResolvedValue({ numberPrefix: '' }),
  },
};

let restoreGlobals;
let AdminCreateCompetition;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_setup.jsx');
  AdminCreateCompetition = window.AdminCreateCompetition;
});

afterAll(() => restoreGlobals());

async function mountForm(tournament) {
  await act(async () => {
    render(
      <AdminCreateCompetition
        tournament={{ name: 'Spring Taikai', courts: ['A', 'B', 'C', 'D'], competitions: [], ...tournament }}
        onCancel={noop} onCreate={noop} onLogout={noop} onViewerMode={noop} password=""
      />
    );
  });
}

describe('the create-competition form names its hand-written fields', () => {
  it('Display name, Day (select) and Start time are reachable by their labels', async () => {
    await mountForm({ date: '10-08-2026', durationDays: 2 });
    expect(screen.getByLabelText('Display name').tagName).toBe('INPUT');
    expect(screen.getByLabelText('Day').tagName).toBe('SELECT');
    expect(screen.getByLabelText('Start time').type).toBe('time');
  });

  it('Day is still named when the tournament has no date and it is a date picker', async () => {
    await mountForm({ date: '' });
    expect(screen.getByLabelText('Day').type).toBe('date');
  });

  it('Assigned shiaijo is a named group whose pills say which are picked', async () => {
    await mountForm({ date: '10-08-2026' });
    const group = screen.getByRole('group', { name: 'Assigned shiaijo (courts)' });
    expect(within(group).getAllByRole('button')).toHaveLength(4);
    // The form starts on A + B.
    expect(pressedNames(group)).toEqual(['Shiaijo (court) A', 'Shiaijo (court) B']);
    await act(async () => { fireEvent.click(within(group).getByRole('button', { name: 'Shiaijo (court) C' })); });
    expect(pressedNames(group)).toContain('Shiaijo (court) C');
  });

  it('Knockout qualifiers is a named group with one pressed pill', async () => {
    await mountForm({ date: '10-08-2026' });
    await act(async () => {
      fireEvent.click(within(screen.getByRole('group', { name: 'Format' })).getByRole('button', { name: 'Pools + Knockout' }));
    });
    await act(async () => {
      fireEvent.click(within(screen.getByRole('group', { name: 'Pool size is a' })).getAllByRole('button')
        .find((b) => /minimum/i.test(b.textContent)));
    });
    const group = screen.getByRole('group', { name: 'Knockout qualifiers' });
    expect(within(group).getAllByRole('button')).toHaveLength(3);
    expect(pressedNames(group)).toHaveLength(1);
  });
});
