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
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).toBeNull();
    expect(submitButton(container).disabled).toBe(false);
  });

  it('hints and blocks submit on 3 shiaijo', async () => {
    const { container, onCreate } = await mountForm();
    await clickPill(container, 'Shiaijo (court) C'); // default A+B → A, B, C

    const hint = container.querySelector('[data-testid="shiaijo-count-error"]');
    expect(hint).not.toBeNull();
    // Same message the Settings screen and the Go side use, with the venue's
    // own counts: this fixture has 4 shiaijo, so 4 is genuinely on offer.
    expect(hint.textContent).toContain('3 shiaijo cannot be paired');
    expect(hint.textContent).toContain('This tournament has 4, so this competition can use 1, 2 or 4');
    expect(submitButton(container).disabled).toBe(true);
    expect(onCreate).not.toHaveBeenCalled();
  });

  it('clears once the allocation is pairable again', async () => {
    const { container } = await mountForm();
    await clickPill(container, 'Shiaijo (court) C');
    expect(submitButton(container).disabled).toBe(true);
    await clickPill(container, 'Shiaijo (court) D'); // → A, B, C, D
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).toBeNull();
    expect(submitButton(container).disabled).toBe(false);
  });

  // The remedy must be reachable AT THIS VENUE. Venue-blind, the count rule
  // offers the nearest legal counts either side, so a 3-shiaijo hall was told
  // to "Use 2 or 4" directly above a hint reading "can use 1 or 2 (this
  // tournament has 3)". One of those is a court the hall does not have.
  it('never offers a count the venue cannot supply', async () => {
    const { container } = await mountForm({ tournament: makeTournament({ courts: ['A', 'B', 'C'] }) });
    await clickPill(container, 'Shiaijo (court) C'); // default A+B -> A, B, C

    const err = container.querySelector('[data-testid="shiaijo-count-error"]');
    expect(err).not.toBeNull();
    expect(err.textContent).toContain('This tournament has 3, so this competition can use 1 or 2');
    expect(err.textContent).not.toContain('4');
    // And it agrees with the standing hint rendered directly below it.
    const standing = container.querySelector('[data-testid="shiaijo-count-hint"]');
    expect(standing.textContent).toContain('can use 1 or 2 shiaijo');
  });

  // Deselecting every pill used to be silently "fixed" at submit: the form sent
  // [safeCourts[0] || "A"], so an operator who cleared the selection on a
  // 4-shiaijo venue got a 1-shiaijo competition with nothing on screen saying
  // so. shiaijoCountError answers null for 0 (an empty list on a STORED record
  // legitimately means "inherit"), so nothing else was going to catch it.
  //
  // Both formats, because the emptiness rule is NOT scoped by format the way
  // the count rule is: a league has to run somewhere too.
  it.each(['playoffs', 'league'])('refuses an empty selection instead of quietly picking a shiaijo (%s)', async (format) => {
    const { container } = await mountForm();
    if (format === 'league') {
      await act(async () => {
        fireEvent.click(Array.from(container.querySelectorAll('button.radio-pill')).find((b) => b.textContent.trim() === 'League'));
      });
    }
    await clickPill(container, 'Shiaijo (court) A');
    await clickPill(container, 'Shiaijo (court) B'); // default A+B → none

    const err = container.querySelector('[data-testid="shiaijo-count-error"]');
    expect(err).not.toBeNull();
    expect(err.textContent).toContain(window.SHIAIJO_NONE_SELECTED);
    expect(submitButton(container).disabled).toBe(true);
  });

  // The guard inside create() agrees with the disabled button, so a programmatic
  // click cannot slip past it either.
  it('refuses the submit itself, not just the button', async () => {
    const { container, onCreate } = await mountForm();
    await clickPill(container, 'Shiaijo (court) A');
    await clickPill(container, 'Shiaijo (court) B');
    await act(async () => { fireEvent.click(submitButton(container)); });
    expect(onCreate).not.toHaveBeenCalled();
  });

  it('recovers as soon as a shiaijo is picked again', async () => {
    const { container } = await mountForm();
    await clickPill(container, 'Shiaijo (court) A');
    await clickPill(container, 'Shiaijo (court) B');
    expect(submitButton(container).disabled).toBe(true);
    await clickPill(container, 'Shiaijo (court) C'); // → C alone: 1 is legal
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).toBeNull();
    expect(submitButton(container).disabled).toBe(false);
  });

  it('leaves a league alone: its courts run in parallel with nothing to merge', async () => {
    const { container } = await mountForm();
    await act(async () => {
      fireEvent.click(Array.from(container.querySelectorAll('button.radio-pill')).find((b) => b.textContent.trim() === 'League'));
    });
    await clickPill(container, 'Shiaijo (court) C');
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).toBeNull();
    expect(submitButton(container).disabled).toBe(false);
  });
});

// The rule used to reach the operator ONLY as a rejection, after a bad pick.
// The standing hint teaches it at the field, before anything can be blocked,
// and is venue-aware so a 3-shiaijo tournament answers "why can't I pick all
// three of my shiaijo" in place.
describe('AdminCreateCompetition standing shiaijo hint (spec 007 R9)', () => {
  const hintText = (container) => {
    const el = container.querySelector('[data-testid="shiaijo-count-hint"]');
    return el && el.textContent;
  };

  it('states the valid counts and the reason on a VALID selection', async () => {
    const { container } = await mountForm();
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).toBeNull();
    const hint = hintText(container);
    expect(hint).not.toBeNull();
    expect(hint).toContain('can use 1, 2 or 4 shiaijo');
    expect(hint).toContain('merge in pairs');
    expect(hint).toContain('halve cleanly');
  });

  it('is venue-aware: a 3-shiaijo tournament offers 1 or 2', async () => {
    const { container } = await mountForm({ tournament: makeTournament({ courts: ['A', 'B', 'C'] }) });
    const hint = hintText(container);
    expect(hint).toContain('can use 1 or 2 shiaijo');
    expect(hint).toContain('this tournament has 3');
  });

  it('stays on screen once the selection goes invalid, without repeating the mechanism', async () => {
    const { container } = await mountForm({ tournament: makeTournament({ courts: ['A', 'B', 'C'] }) });
    await clickPill(container, 'Shiaijo (court) C'); // → A, B, C: invalid
    expect(container.querySelector('[data-testid="shiaijo-count-error"]')).not.toBeNull();
    const hint = hintText(container);
    expect(hint).toContain('can use 1 or 2 shiaijo');
    // The red error one line above already states it; twice is noise.
    expect(hint).not.toContain('halve cleanly');
  });

  it('is absent for league, which the rule does not govern', async () => {
    const { container } = await mountForm();
    await act(async () => {
      fireEvent.click(Array.from(container.querySelectorAll('button.radio-pill')).find((b) => b.textContent.trim() === 'League'));
    });
    expect(container.querySelector('[data-testid="shiaijo-count-hint"]')).toBeNull();
  });
});

describe('AdminCreateCompetition server-error surfacing (bc-draw R9 gap 2)', () => {
  const errorBanner = (container) => container.querySelector('.alert--error');

  // The mocked rejection is the LIVE server message, verbatim: POST
  // /api/competitions prefixes "courts: " (handlers_competition.go) onto
  // helper.ValidateShiaijoCount's text for a 3-shiaijo allocation. Mocking a
  // retired string (the pre-R9 "must be 1 or an even number") still proved the
  // banner renders SOMETHING, but no longer proved a real rejection reaches it.
  it('shows the server rejection in the form banner instead of nothing', async () => {
    const onCreate = vi.fn().mockRejectedValue(new Error(
      'courts: shiaijo count must be a power of two (1, 2, 4, 8 or 16), got 3: use 2 or 4, or 1; '
      + 'the knockout draw gives each shiaijo its own block of the bracket and the blocks merge in pairs, '
      + 'so the count has to halve cleanly'));
    const { container } = await mountForm({ onCreate });

    await act(async () => { fireEvent.click(submitButton(container)); });

    await waitFor(() => expect(errorBanner(container)).not.toBeNull());
    expect(errorBanner(container).textContent).toContain('shiaijo count must be a power of two');
    // The remedy half must survive too: a truncated banner that stops at the
    // rule leaves the operator without the counts they can actually pick.
    expect(errorBanner(container).textContent).toContain('use 2 or 4, or 1');
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
