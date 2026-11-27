// The Posters page's address (bc-qrps): /admin/posters opens it and the page
// writes /admin/posters back, so a reload or a shared link lands on it.
import { describe, it, expect } from 'vitest';
import { parsePath, pathFromState } from '../app.jsx';

describe('/admin/posters', () => {
  it('opens the Posters page', () => {
    expect(parsePath('/admin/posters')).toEqual({ mode: 'admin', admin: { kind: 'posters' } });
  });

  it('is the address the Posters page writes', () => {
    expect(pathFromState('admin', undefined, undefined, { kind: 'posters' }, undefined)).toBe('/admin/posters');
  });

  it('round-trips', () => {
    const { mode, admin } = parsePath('/admin/posters');
    expect(pathFromState(mode, undefined, undefined, admin, undefined)).toBe('/admin/posters');
  });
});
