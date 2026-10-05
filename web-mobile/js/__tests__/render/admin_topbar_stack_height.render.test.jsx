// bc-tmfd: AdminTopbar publishes its stack's height as --topbar-stack-h, which
// the team sheet's pinned header reads to sit directly under the topbar.
// Measured once in a layout effect (a ResizeObserver first fires after paint),
// then kept current by the observer, and removed when the topbar unmounts.

import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';

const PROP = '--topbar-stack-h';
const rootValue = () => document.documentElement.style.getPropertyValue(PROP);

let AdminTopbar;
let height;
let observers;
const realRO = globalThis.ResizeObserver;
const realRect = Element.prototype.getBoundingClientRect;

beforeAll(async () => {
  await import('../../admin_shell.jsx');
  AdminTopbar = window.AdminTopbar;
});

beforeEach(() => {
  height = 65;
  observers = [];
  window.subscribeSyncStatus = (fn) => { fn('synced'); return () => {}; };
  window.subscribeUnsentWrites = undefined;
  Element.prototype.getBoundingClientRect = function () {
    return this.classList && this.classList.contains('topbar-stack')
      ? { top: 0, bottom: height, height, left: 0, right: 0, width: 0, x: 0, y: 0 }
      : realRect.call(this);
  };
});

afterEach(() => {
  Element.prototype.getBoundingClientRect = realRect;
  globalThis.ResizeObserver = realRO;
  document.documentElement.style.removeProperty(PROP);
});

const mountBar = () => render(
  <AdminTopbar onLogout={() => {}} onViewerMode={() => {}} tournament={null} hideRunningStrip />
);

function stubObserver() {
  globalThis.ResizeObserver = class {
    constructor(cb) { this.cb = cb; this.observed = []; this.disconnected = false; observers.push(this); }
    observe(el) { this.observed.push(el); }
    disconnect() { this.disconnected = true; }
  };
}

describe('AdminTopbar publishes --topbar-stack-h', () => {
  it('sets the measured border-box height before any observer callback fires', () => {
    stubObserver();
    mountBar();
    expect(rootValue()).toBe('65px');
    expect(observers).toHaveLength(1);
    expect(observers[0].observed[0].classList.contains('topbar-stack')).toBe(true);
  });

  it('updates when the observer fires with a grown stack', () => {
    stubObserver();
    mountBar();
    height = 118;
    act(() => { observers[0].cb([]); });
    expect(rootValue()).toBe('118px');
  });

  it('removes the property and disconnects on unmount', () => {
    stubObserver();
    const { unmount } = mountBar();
    expect(rootValue()).toBe('65px');
    unmount();
    expect(rootValue()).toBe('');
    expect(observers[0].disconnected).toBe(true);
  });

  it('still measures once, and does not throw, without ResizeObserver', () => {
    globalThis.ResizeObserver = undefined;
    let bar;
    expect(() => { bar = mountBar(); }).not.toThrow();
    expect(rootValue()).toBe('65px');
    bar.unmount();
    expect(rootValue()).toBe('');
  });
});
