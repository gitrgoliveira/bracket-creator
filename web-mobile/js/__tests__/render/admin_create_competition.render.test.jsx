import React from 'react';
import { render, act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// bc-draw R9 UAT gap 2: the "Add competition" form had NO shiaijo-count guard
// and swallowed server rejections.
//
// Selecting 3 shiaijo showed no hint, left "Create & continue" live, POSTed,
// and took a 400 whose only trace was a bottom-of-screen toast that expires
// after 8s. Once it expired the operator was looking at a dead button with no
// explanation anywhere on the form: every other failure on this form reports
// through its own `.alert--error` banner.
//
// Mounted with REAL React (this project) so the component's own state machine
// runs; window.* deps captured at module load are stubbed before the import.

const noop = () => {};
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
  // Real helpers (normalizeCourts, shiaijoCountError, formatDrawsBracket,
  // decideNumericUpdate, deriveTournamentDays, validateAndNormalizeDate,
  // MAX_* constants) come from admin_helpers.jsx, which the render harness
  // loads in vitest.setup.render.js. Stubbing them here would let the form
  // drift from the rule it is supposed to mirror.
  buildCompetition: (cfg) => ({ ...cfg }),
  addMinutes: (t) => t,
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
  },
};

const originals = {};
let AdminCreateCompetition;
let AdminEditTournament;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_setup.jsx');
  AdminCreateCompetition = window.AdminCreateCompetition;
  AdminEditTournament = window.AdminEditTournament;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

function makeTournament(overrides = {}) {
  return {
    name: 'Spring Taikai',
    date: '10-08-2026',
    durationDays: 1,
    courts: ['A', 'B', 'C', 'D'],
    competitions: [],
    ...overrides,
  };
}

async function mountForm({ tournament = makeTournament(), onCreate = vi.fn() } = {}) {
  let result;
  await act(async () => {
    result = render(
      <AdminCreateCompetition
        tournament={tournament}
        onCancel={noop}
        onCreate={onCreate}
        onLogout={noop}
        onViewerMode={noop}
        password=""
      />
    );
  });
  return { ...result, onCreate };
}

const submitButton = (container) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent.includes('Create & continue'));

const clickPill = async (container, label) => {
  const pill = Array.from(container.querySelectorAll('button.radio-pill'))
    .find((b) => b.textContent.trim() === label);
  expect(pill, `pill "${label}" not found`).not.toBeUndefined();
  await act(async () => { fireEvent.click(pill); });
};

describe('AdminCreateCompetition shiaijo-count guard (bc-draw R9 gap 2)', () => {
  it('starts on a pairable 2-shiaijo default with the button live', async () => {
    const { container } = await mountForm();
    expect(container.querySelector('[data-testid="odd-shiaijo-hint"]')).toBeNull();
    expect(submitButton(container).disabled).toBe(false);
  });

  it('hints and blocks submit on 3 shiaijo', async () => {
    const { container, onCreate } = await mountForm();
    await clickPill(container, 'Shiaijo (court) C'); // default A+B → A, B, C

    const hint = container.querySelector('[data-testid="odd-shiaijo-hint"]');
    expect(hint).not.toBeNull();
    // Same message the Settings screen and the Go side use.
    expect(hint.textContent).toContain('3 shiaijo cannot be paired');
    expect(hint.textContent).toContain('Use 2 or 4, or 1');
    expect(submitButton(container).disabled).toBe(true);
    expect(onCreate).not.toHaveBeenCalled();
  });

  it('clears once the allocation is pairable again', async () => {
    const { container } = await mountForm();
    await clickPill(container, 'Shiaijo (court) C');
    expect(submitButton(container).disabled).toBe(true);
    await clickPill(container, 'Shiaijo (court) D'); // → A, B, C, D
    expect(container.querySelector('[data-testid="odd-shiaijo-hint"]')).toBeNull();
    expect(submitButton(container).disabled).toBe(false);
  });

  it('leaves a league alone: its courts are parallel mats with nothing to pair', async () => {
    const { container } = await mountForm();
    await act(async () => {
      fireEvent.click(Array.from(container.querySelectorAll('button.radio-pill')).find((b) => b.textContent.trim() === 'League'));
    });
    await clickPill(container, 'Shiaijo (court) C');
    expect(container.querySelector('[data-testid="odd-shiaijo-hint"]')).toBeNull();
    expect(submitButton(container).disabled).toBe(false);
  });
});

describe('AdminCreateCompetition server-error surfacing (bc-draw R9 gap 2)', () => {
  const errorBanner = (container) => container.querySelector('.alert--error');

  it('shows the server rejection in the form banner instead of nothing', async () => {
    const onCreate = vi.fn().mockRejectedValue(new Error('courts: courts must be 1 or an even number, got 3'));
    const { container } = await mountForm({ onCreate });

    await act(async () => { fireEvent.click(submitButton(container)); });

    await waitFor(() => expect(errorBanner(container)).not.toBeNull());
    expect(errorBanner(container).textContent).toContain('courts must be 1 or an even number');
    // The button comes back so the operator can retry after fixing the form.
    expect(submitButton(container).disabled).toBe(false);
  });

  it('surfaces ANY server rejection, not just the court rule', async () => {
    const onCreate = vi.fn().mockRejectedValue(new Error('number prefix "A" already used by competition "Womens"'));
    const { container } = await mountForm({ onCreate });

    await act(async () => { fireEvent.click(submitButton(container)); });

    await waitFor(() => expect(errorBanner(container)).not.toBeNull());
    expect(errorBanner(container).textContent).toContain('number prefix');
  });

  it('falls back to a readable message when the rejection carries none', async () => {
    const onCreate = vi.fn().mockRejectedValue(new Error(''));
    const { container } = await mountForm({ onCreate });

    await act(async () => { fireEvent.click(submitButton(container)); });

    await waitFor(() => expect(errorBanner(container)).not.toBeNull());
    expect(errorBanner(container).textContent).toContain('Could not create the competition');
  });

  it('clears a stale rejection as soon as the operator changes the shiaijo', async () => {
    const onCreate = vi.fn().mockRejectedValue(new Error('boom'));
    const { container } = await mountForm({ onCreate });
    await act(async () => { fireEvent.click(submitButton(container)); });
    await waitFor(() => expect(errorBanner(container)).not.toBeNull());

    await clickPill(container, 'Shiaijo (court) D');
    expect(errorBanner(container)).toBeNull();
  });

  it('shows no banner when the create succeeds', async () => {
    const onCreate = vi.fn().mockResolvedValue({ id: 'c1' });
    const { container } = await mountForm({ onCreate });

    await act(async () => { fireEvent.click(submitButton(container)); });

    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(errorBanner(container)).toBeNull();
  });
});

// bc-draw R9 UAT gap 3, operator-facing half. The chosen mechanism is to
// REFUSE a tournament court reduction while a live competition still holds a
// removed shiaijo, so the refusal has to actually land: it names which
// competition blocks it and what to do. Before this, every server rejection on
// this form was swallowed into a `false` return by AdminApp.updateTournament,
// leaving only an 8-second toast at the bottom of a long form.
describe('AdminEditTournament server-error surfacing (bc-draw R9 gap 3)', () => {
  const mountEditor = async (onSave) => {
    let result;
    await act(async () => {
      result = render(
        <AdminEditTournament
          // venue has no `|| ""` fallback in the component's state init, so
          // the editor needs a fuller record than the create form does.
          tournament={makeTournament({ venue: 'Kendo Hall', mode: 'officiated' })}
          onCancel={noop}
          onSave={onSave}
          onLogout={noop}
          onViewerMode={noop}
          authConfig={{ mode: 'file' }}
          password="pw"
          showToast={noop}
        />
      );
    });
    return result;
  };

  const save = async (container) => {
    const btn = Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === 'Save changes');
    expect(btn, 'Save changes button not found').not.toBeUndefined();
    await act(async () => { fireEvent.click(btn); });
  };

  it('shows the orphaned-shiaijo refusal in the form banner', async () => {
    const msg = 'cannot set the tournament\'s shiaijo to A, B, C: "Mudansha" still runs on shiaijo D. Reassign those shiaijo in the competition\'s settings first, then change the tournament';
    const onSave = vi.fn().mockRejectedValue(new Error(msg));
    const { container } = await mountEditor(onSave);

    await save(container);

    const banner = await waitFor(() => {
      const el = container.querySelector('[role="alert"]');
      expect(el).not.toBeNull();
      return el;
    });
    expect(banner.textContent).toContain('still runs on shiaijo D');
    expect(banner.textContent).toContain('Reassign');
  });

  it('surfaces ANY tournament-save rejection, not just the court rule', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('tournament mode cannot be changed after creation'));
    const { container } = await mountEditor(onSave);

    await save(container);

    await waitFor(() => expect(container.querySelector('[role="alert"]')).not.toBeNull());
    expect(container.querySelector('[role="alert"]').textContent).toContain('mode cannot be changed');
  });

  it('shows no banner when the save succeeds', async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    const { container } = await mountEditor(onSave);

    await save(container);

    expect(onSave).toHaveBeenCalledTimes(1);
    expect(container.querySelector('[role="alert"]')).toBeNull();
  });
});
