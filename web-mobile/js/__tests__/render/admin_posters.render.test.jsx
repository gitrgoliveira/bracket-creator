// The Posters page (bc-qrps): the poster list, the choices beside a live
// preview, a Save button that alone sends anything, the leave warning, and the
// two Print buttons. jsdom has no print: Print is checked up to the print root
// and window.print; the sheet itself is checked in a real browser.
import React from 'react';
import { createPortal } from 'react-dom';
import { render, screen, fireEvent, act, waitFor, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { AdminPostersPage, LEAVE_POSTERS_ANYWAY, POSTERS_CHANGED_ELSEWHERE, PRINTING_CLASS } from '../../admin_posters.jsx';
import { POSTER_RULES } from '../../posters_model.jsx';
import { UNSAVED_ENTRIES_DISCARDED } from '../../unsaved_entries.jsx';

const base = (extra) => ({
  name: 'Spring Open',
  publicURL: 'https://kendo.example.org',
  courts: ['A', 'B'],
  theme: { primaryColor: '#1d3557', hasLogo: true },
  sponsors: [{ name: 'Dojo Supplies', file: 'sp1.png' }],
  tournamentVersion: 'v1',
  ...extra,
});

let saves;
let onSave;
let onBack;
let onEditTournament;
let savedReactDOM;

beforeAll(() => {
  savedReactDOM = window.ReactDOM;
  window.ReactDOM = { ...savedReactDOM, createPortal };
});
afterAll(() => { window.ReactDOM = savedReactDOM; });

beforeEach(() => {
  saves = [];
  onSave = vi.fn(async (patch, opts) => { saves.push({ patch, opts }); return true; });
  onBack = vi.fn();
  onEditTournament = vi.fn();
  window.confirmDialog = vi.fn(async () => true);
  window.print = vi.fn();
});
afterEach(() => {
  cleanup();
  delete window.confirmDialog;
  document.body.classList.remove(PRINTING_CLASS);
});

const mount = (t = base()) => {
  const props = { tournament: t, onSave, onBack, onEditTournament, onLogout: vi.fn(), onViewerMode: vi.fn() };
  const view = render(<AdminPostersPage {...props} />);
  return { ...view, rerenderWith: (next) => view.rerender(<AdminPostersPage {...props} tournament={next} />) };
};

const preview = () => screen.getByTestId('posters-preview');
const previewHeading = () => preview().querySelector('.poster__heading').textContent;
const headingBox = () => screen.getByLabelText('Heading');
const ctaBox = () => screen.getByLabelText(/Call to action/);
const type = (box, value) => fireEvent.change(box, { target: { value } });
const status = () => screen.getByTestId('posters-status').textContent;
const tap = async (name) => { await act(async () => { fireEvent.click(screen.getByRole('button', { name })); }); };

describe('the poster list and the choices', () => {
  it('lists the viewer home, then each court in the tournament order', () => {
    mount(base({ courts: ['B', 'A'] }));
    const group = screen.getByRole('group', { name: 'Poster' });
    expect([...group.querySelectorAll('button')].map((b) => b.textContent)).toEqual(['Viewer home', 'Shiaijo B', 'Shiaijo A']);
  });

  it('shows a typed heading in the preview before any save', () => {
    mount();
    type(headingBox(), 'Scan for live results');
    expect(previewHeading()).toBe('Scan for live results');
    expect(onSave).not.toHaveBeenCalled();
  });

  it('falls back to the default heading when the box is empty', () => {
    mount(base({ posters: { viewer: { heading: 'Old' } } }));
    type(headingBox(), '   ');
    expect(previewHeading()).toBe(POSTER_RULES.defaultHeadings.viewer);
    fireEvent.click(screen.getByRole('button', { name: 'Shiaijo A' }));
    expect(previewHeading()).toBe(POSTER_RULES.defaultHeadings.court);
    expect(headingBox()).toHaveAttribute('placeholder', POSTER_RULES.defaultHeadings.court);
  });

  it('counts each box against the shared limits', () => {
    mount();
    expect(screen.getByTestId('poster-viewer-heading-count').textContent).toBe(`0 of ${POSTER_RULES.headingMaxLength} characters`);
    expect(screen.getByTestId('poster-viewer-cta-count').textContent).toBe(`0 of ${POSTER_RULES.callToActionMaxLength} characters`);
    type(headingBox(), 'x'.repeat(POSTER_RULES.headingMaxLength + 2));
    const over = screen.getByTestId('poster-viewer-heading-count');
    expect(over.textContent).toContain('2 too many');
    expect(over).toHaveClass('field__hint--error');
    type(ctaBox(), 'y'.repeat(POSTER_RULES.callToActionMaxLength));
    expect(screen.getByTestId('poster-viewer-cta-count')).not.toHaveClass('field__hint--error');
  });

  it('counts an emoji as one character', () => {
    mount();
    type(headingBox(), '\u{1F38C}');
    expect(screen.getByTestId('poster-viewer-heading-count').textContent).toBe(`1 of ${POSTER_RULES.headingMaxLength} characters`);
  });

  it('offers no logo choice when no logo is uploaded', () => {
    mount(base({ theme: { primaryColor: '#1d3557', hasLogo: false } }));
    expect(screen.queryByLabelText('Logo in the centre of the code')).toBeNull();
    expect(screen.queryByLabelText('Logo above the code')).toBeNull();
  });

  it('offers both logo places for a short address, centre first', () => {
    mount();
    expect(screen.getByLabelText('Logo in the centre of the code')).toBeChecked();
    expect(screen.getByLabelText('Logo in the centre of the code')).not.toBeDisabled();
    fireEvent.click(screen.getByLabelText('Logo above the code'));
    expect(preview().querySelector('.poster').getAttribute('data-placement')).toBe('above');
  });

  it('disables Centre, and says why, for an address too long for a logo in the code', () => {
    mount(base({ publicURL: 'https://' + 'a'.repeat(130) + '.example.org' }));
    const centre = screen.getByLabelText('Logo in the centre of the code');
    expect(centre).toBeDisabled();
    expect(screen.getByLabelText('Logo above the code')).toBeChecked();
    expect(screen.getByText(/too long to carry a logo in the middle of the code/)).toBeInTheDocument();
  });

  it('does not say the centre was refused when the organiser chose Above', () => {
    mount(base({ publicURL: 'https://' + 'a'.repeat(130) + '.example.org', posters: { viewer: { logo: 'above' } } }));
    expect(screen.getByLabelText('Logo in the centre of the code')).toBeDisabled();
    expect(screen.getByLabelText('Logo above the code')).toBeChecked();
    expect(screen.queryByText(/too long to carry a logo in the middle of the code/)).toBeNull();
  });

  it('offers no sponsor switch without sponsors', () => {
    mount(base({ sponsors: [] }));
    expect(screen.queryByLabelText('Show sponsors')).toBeNull();
  });

  it('hides the sponsor strip of the preview when Show sponsors is cleared', () => {
    mount();
    expect(preview().querySelector('.poster__sponsors')).not.toBeNull();
    fireEvent.click(screen.getByLabelText('Show sponsors'));
    expect(preview().querySelector('.poster__sponsors')).toBeNull();
  });
});

describe('where the logo and the sponsors come from', () => {
  it('says they come from Edit details, and leads there', async () => {
    mount();
    expect(screen.getByTestId('posters-branding')).toHaveTextContent('The logo and the sponsor images come from the Branding and Sponsors sections of Edit details.');
    await tap('Open Edit details');
    expect(onEditTournament).toHaveBeenCalledTimes(1);
  });

  it('says where to add them when there are none', () => {
    mount(base({ sponsors: [], theme: { hasLogo: false } }));
    expect(screen.getByTestId('posters-branding')).toHaveTextContent('To put a logo or sponsor images on the posters, add them in the Branding and Sponsors sections of Edit details.');
  });
});

describe('the public-address warning', () => {
  it('shows when no public address is set and this address is not public, and leads to Edit details', async () => {
    mount(base({ publicURL: '' }));
    const warning = screen.getByTestId('posters-address-warning');
    expect(warning).toHaveClass('poster-no-print');
    await tap('Set it in Edit details');
    expect(onEditTournament).toHaveBeenCalledTimes(1);
  });

  it('is not shown with a public address set', () => {
    mount();
    expect(screen.queryByTestId('posters-address-warning')).toBeNull();
  });
});

describe('Save', () => {
  it('sends nothing while typing and switching, then one save with the whole draft at the version read', async () => {
    mount();
    type(headingBox(), 'Scan me');
    fireEvent.click(screen.getByRole('button', { name: 'Shiaijo B' }));
    type(ctaBox(), 'Results here');
    expect(onSave).not.toHaveBeenCalled();
    expect(status()).toBe('Unsaved changes');

    await tap('Save');
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(saves[0].patch).toEqual({
      tournamentVersion: 'v1',
      posters: { viewer: { heading: 'Scan me' }, courts: { B: { callToAction: 'Results here' } } },
    });
    expect(status()).toBe('Saving');

    await act(async () => { saves[0].opts.onSettled({ tournamentVersion: 'v2' }); });
    expect(status()).toBe('Saved');

    // The next save is made against the version the last one answered.
    type(ctaBox(), 'Results here now');
    await tap('Save');
    expect(saves[1].patch.tournamentVersion).toBe('v2');
  });

  it('sends nothing when nothing is unsaved', async () => {
    mount();
    expect(status()).toBe('Saved');
    await tap('Save');
    expect(onSave).not.toHaveBeenCalled();
    expect(status()).toBe('Saved');
  });

  it('trailing spaces alone are not a change', async () => {
    mount(base({ posters: { viewer: { heading: 'Scan me' } } }));
    type(headingBox(), 'Scan me  ');
    expect(status()).toBe('Saved');
  });

  it('clearing the last choice sends posters: {}, never an omitted key', async () => {
    mount(base({ posters: { viewer: { heading: 'Old' } } }));
    type(headingBox(), '');
    await tap('Save');
    expect(saves[0].patch).toHaveProperty('posters');
    expect(saves[0].patch.posters).toEqual({});
  });

  it('a refusal made against an older copy keeps the draft and offers Save again at the current version', async () => {
    const view = mount();
    type(headingBox(), 'Scan me');
    await tap('Save');
    const e = Object.assign(new Error('changed'), { code: 'tournament_changed' });
    let claimed;
    await act(async () => { claimed = saves[0].opts.onRefusal(e); });
    expect(claimed).toBe(true);
    expect(screen.getByTestId('posters-refusal').textContent).toBe(POSTERS_CHANGED_ELSEWHERE);
    expect(headingBox()).toHaveValue('Scan me');
    expect(status()).toBe('Unsaved changes');

    // The app took the current copy back: the page's draft stays.
    view.rerenderWith(base({ tournamentVersion: 'v9', posters: { viewer: { heading: 'From elsewhere' } } }));
    expect(headingBox()).toHaveValue('Scan me');

    await tap('Save again');
    expect(saves[1].patch).toEqual({ tournamentVersion: 'v9', posters: { viewer: { heading: 'Scan me' } } });
    expect(screen.queryByTestId('posters-refusal')).toBeNull();
  });

  it('a 400 shows the server\'s sentence', async () => {
    mount();
    type(headingBox(), 'Scan me');
    await tap('Save');
    await act(async () => { saves[0].opts.onRefusal(new Error('The heading on the Viewer home poster is longer than 80 characters.')); });
    expect(screen.getByTestId('posters-refusal').textContent).toContain('The heading on the Viewer home poster is longer than 80 characters.');
    expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
  });

  it('a save discarded from the held writes is unsaved again', async () => {
    mount();
    type(headingBox(), 'Scan me');
    await tap('Save');
    await act(async () => { saves[0].opts.onSettled(null); });
    expect(status()).toBe('Unsaved changes');
  });
});

describe('what counts as unsaved', () => {
  it('a save the outbox holds is not an unsaved entry: leaving does not warn and a reload is not stopped', async () => {
    mount();
    type(headingBox(), 'Scan me');
    await tap('Save');
    // No onSettled yet: the write is queued (offline) or in flight.
    expect(status()).toBe('Saving');
    const ev = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Back' }));
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it('a held save the server then refuses is unsaved again, and leaving asks', async () => {
    mount();
    type(headingBox(), 'Scan me');
    await tap('Save');
    await act(async () => { saves[0].opts.onRefusal(new Error('The server refused it.')); });
    expect(status()).toBe('Unsaved changes');
    await tap('Back');
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
  });

  it('two saves out at once, both refused, turn the page back to what the server holds', async () => {
    mount();
    type(headingBox(), 'First');
    await tap('Save');
    type(headingBox(), 'Second');
    await tap('Save');
    await act(async () => { saves[0].opts.onRefusal(new Error('The server refused it.')); });
    await act(async () => { saves[1].opts.onRefusal(new Error('The server refused it.')); });
    // The server holds no poster text: the first save's text is not stored.
    type(headingBox(), 'First');
    expect(status()).toBe('Unsaved changes');
  });

  it('an older save refused while a newer one is out leaves the newer one as sent', async () => {
    mount();
    type(headingBox(), 'First');
    await tap('Save');
    type(headingBox(), 'Second');
    await tap('Save');
    await act(async () => { saves[0].opts.onRefusal(new Error('The server refused it.')); });
    expect(status()).toBe('Saving');
    await act(async () => { saves[1].opts.onSettled({ tournamentVersion: 'v2' }); });
    expect(status()).toBe('Saved');
  });
});

describe('text over a limit', () => {
  it('disables Save and says why until the text is shortened', async () => {
    mount();
    type(headingBox(), 'x'.repeat(POSTER_RULES.headingMaxLength + 1));
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    expect(screen.getByTestId('posters-too-long')).toBeInTheDocument();
    type(headingBox(), 'x'.repeat(POSTER_RULES.headingMaxLength));
    expect(screen.getByRole('button', { name: 'Save' })).not.toBeDisabled();
    expect(screen.queryByTestId('posters-too-long')).toBeNull();
    await tap('Save');
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it('holds for a poster that is not the one on screen, and for the call to action', () => {
    mount();
    type(ctaBox(), 'y'.repeat(POSTER_RULES.callToActionMaxLength + 1));
    fireEvent.click(screen.getByRole('button', { name: 'Shiaijo B' }));
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  });
});

describe('leaving', () => {
  it('Back with unsaved changes asks first: Keep entries stays with the draft', async () => {
    window.confirmDialog = vi.fn(async () => false);
    mount();
    type(headingBox(), 'Scan me');
    await tap('Back');
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0]).toMatchObject({
      message: `${UNSAVED_ENTRIES_DISCARDED} ${LEAVE_POSTERS_ANYWAY}`,
      confirmLabel: 'Discard entries',
      cancelLabel: 'Keep entries',
    });
    expect(onBack).not.toHaveBeenCalled();
    expect(headingBox()).toHaveValue('Scan me');
  });

  it('Discard entries leaves', async () => {
    mount();
    type(headingBox(), 'Scan me');
    await tap('Back');
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it('a running-match chip in the topbar asks first through beforeLeave, and goes at once with nothing unsaved', async () => {
    let leave;
    window.AdminTopbar = (props) => { leave = props.beforeLeave; return null; };
    try {
      window.confirmDialog = vi.fn(async () => false);
      mount();
      expect(leave()).toBe(true);
      type(headingBox(), 'Scan me');
      let answer;
      await act(async () => { answer = leave(); });
      expect(window.confirmDialog).toHaveBeenCalledTimes(1);
      expect(await answer).toBe(false);
      window.confirmDialog = vi.fn(async () => true);
      await act(async () => { answer = leave(); });
      expect(await answer).toBe(true);
    } finally {
      delete window.AdminTopbar;
    }
  });

  it('Back with nothing unsaved leaves at once', () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Back' }));
    expect(onBack).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog).not.toHaveBeenCalled();
  });

  it('a reload with unsaved changes gets the browser\'s prompt, and none once saved', async () => {
    mount();
    const unload = () => { const ev = new Event('beforeunload', { cancelable: true }); window.dispatchEvent(ev); return ev.defaultPrevented; };
    expect(unload()).toBe(false);
    type(headingBox(), 'Scan me');
    expect(unload()).toBe(true);
    await tap('Save');
    await act(async () => { saves[0].opts.onSettled({ tournamentVersion: 'v2' }); });
    expect(unload()).toBe(false);
  });
});

describe('Print', () => {
  const printRoot = () => document.body.querySelector(':scope > .poster-print-root');

  it('prints the unsaved draft of this poster and sends nothing', async () => {
    mount();
    type(headingBox(), 'Draft heading');
    await tap('Print this poster');
    expect(document.body).toHaveClass(PRINTING_CLASS);
    const posters = printRoot().querySelectorAll('.poster');
    expect(posters).toHaveLength(1);
    expect(posters[0].querySelector('.poster__heading').textContent).toBe('Draft heading');
    await waitFor(() => expect(window.print).toHaveBeenCalledTimes(1));
    expect(onSave).not.toHaveBeenCalled();
    expect(status()).toBe('Unsaved changes');

    await act(async () => { window.dispatchEvent(new Event('afterprint')); });
    expect(printRoot()).toBeNull();
    expect(document.body).not.toHaveClass(PRINTING_CLASS);
  });

  it('Print all renders every poster, in order, into the print root', async () => {
    mount();
    await tap('Print all posters');
    expect([...printRoot().querySelectorAll('.poster')].map((p) => p.getAttribute('data-poster'))).toEqual(['viewer', 'court:A', 'court:B']);
    await waitFor(() => expect(window.print).toHaveBeenCalledTimes(1));
    await act(async () => { window.dispatchEvent(new Event('afterprint')); });
    expect(printRoot()).toBeNull();
  });
});
