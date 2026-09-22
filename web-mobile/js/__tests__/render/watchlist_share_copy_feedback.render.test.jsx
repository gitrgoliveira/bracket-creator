// bc-wlpl: the share sheet answers "did that copy?" in BOTH directions.
//
// This file exists because the failing direction had no answer at all. The
// sheet passed `onCopy={setCopied}` straight into ShareLinkModal, which calls
// `onCopy(false)` when the clipboard refuses -- so a failure set the state back
// to its untouched value and the sheet said nothing. That is exactly the
// deployment this app is built for: plain http on a venue LAN, where
// navigator.clipboard does not exist at all and the execCommand fallback can
// still be refused. The one action on the sheet then failed in silence, with
// the URL sitting right there unselected.
//
// A RENDER test rather than a unit one: the bug lived in the wiring between a
// state setter and a callback's argument, which a vnode-shaped assertion can
// reproduce without ever running the setter.
import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';

// A stand-in for ui.jsx's ShareLinkModal: the real one is script-tagged and
// pulls in a QR canvas. What matters here is its CONTRACT -- it renders the
// caller's children and reports the outcome as a boolean -- so the stub is
// that contract and nothing else. Both outcomes are reachable from the UI.
const ShareLinkModalStub = ({ title, url, onCopy, children }) => (
  <div>
    <div>{title}</div>
    <div data-testid="share-link-url">{url}</div>
    <button type="button" onClick={() => onCopy(true)}>copy-ok</button>
    <button type="button" onClick={() => onCopy(false)}>copy-fail</button>
    {children}
  </div>
);

let WatchlistShareModal;

beforeAll(async () => {
  window.ShareLinkModal = ShareLinkModalStub;
  ({ WatchlistShareModal } = await import('../../viewer_watchlist.jsx'));
});
afterAll(() => { delete window.ShareLinkModal; });
beforeEach(() => cleanup());

const WATCHLIST = [{ type: 'player', id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' }];
const ROSTER = [{ id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo', number: 'K12' }];

function mount() {
  render(
    <WatchlistShareModal base="http://venue.local/" watchlist={WATCHLIST} roster={ROSTER} onClose={vi.fn()} />
  );
}

describe('the watchlist share sheet reports the copy outcome', () => {
  it('says nothing before the reader presses Copy', () => {
    mount();
    expect(screen.queryByText(/copied/i)).toBeNull();
    expect(screen.queryByText(/copy failed/i)).toBeNull();
  });

  it('confirms a copy that worked', async () => {
    mount();
    await act(async () => { fireEvent.click(screen.getByText('copy-ok')); });
    expect(screen.getByText('Copied.')).toBeTruthy();
    expect(screen.queryByText(/copy failed/i)).toBeNull();
  });

  it('SAYS SO when the copy failed, and tells the reader what to do instead', () => {
    // The regression. With the boolean state this rendered nothing whatsoever.
    mount();
    act(() => { fireEvent.click(screen.getByText('copy-fail')); });
    const note = screen.getByText(/copy failed/i);
    expect(note.textContent).toContain('select the link above manually');
    expect(screen.queryByText('Copied.'), 'and does not also claim success').toBeNull();
    // Announced, not just drawn: the sheet has no other visible reaction.
    expect(note.getAttribute('role')).toBe('status');
  });

  it('replaces one verdict with the other rather than stacking them', () => {
    mount();
    act(() => { fireEvent.click(screen.getByText('copy-fail')); });
    act(() => { fireEvent.click(screen.getByText('copy-ok')); });
    expect(screen.getByText('Copied.')).toBeTruthy();
    expect(screen.queryByText(/copy failed/i)).toBeNull();
  });

  it('still hands the sheet a link built from the watchlist', () => {
    // Guards the stub from drifting into a test of nothing.
    mount();
    expect(screen.getByTestId('share-link-url').textContent).toBe('http://venue.local/?w=K12');
  });
});
