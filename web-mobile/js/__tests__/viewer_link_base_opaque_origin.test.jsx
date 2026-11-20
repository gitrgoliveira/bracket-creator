// linkBase on an opaque origin. The document is moved to about:blank, whose
// origin is the string "null", so the case is a real opaque origin rather than
// a faked `window.location` (which a VM pool does not let a test redefine).
//
// It is not done with a per-file `@vitest-environment-options` url: vitest's
// jsdom environment reads every window property while it starts, and jsdom's
// `localStorage` getter throws on an opaque origin, so under the forks pool
// the worker never starts. `jsdom.reconfigure` after the environment is up
// works under every pool. The file is apart from viewer.test.jsx so the changed
// URL can never reach another file's tests, and the URL is put back afterwards.
import { describe, it, expect } from 'vitest';
import { linkBase } from '../viewer.jsx';

describe('linkBase', () => {
  it('returns empty string when publicURL is absent and origin is opaque ("null")', () => {
    const before = window.location.href;
    try {
      globalThis.jsdom.reconfigure({ url: 'about:blank' });
      // Precondition: the document really has an opaque origin.
      expect(window.location.origin).toBe('null');
      expect(linkBase({})).toBe('');
    } finally {
      globalThis.jsdom.reconfigure({ url: before });
    }
    expect(window.location.href).toBe(before);
  });
});
