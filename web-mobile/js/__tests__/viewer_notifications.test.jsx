// mp-xhaa: unit tests for notifEnable/notifDisable helpers.
// These pure helpers drive AnnBellBtn, NotificationSettings, and the
// NOTIF_SYNC_EVENT broadcast — cover every return value branch.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { notifEnable, notifDisable } from '../viewer.jsx';

// ---------------------------------------------------------------------------
// localStorage stub — window.localStorage is undefined in jsdom/node here.
// Same pattern as app_announcement.test.jsx.
// ---------------------------------------------------------------------------

function makeLocalStorageMock(initial = {}) {
  const store = { ...initial };
  return {
    getItem: (k) => (k in store ? store[k] : null),
    setItem: vi.fn((k, v) => { store[k] = String(v); }),
    removeItem: (k) => { delete store[k]; },
    clear: () => { Object.keys(store).forEach(k => delete store[k]); },
    get _store() { return store; },
  };
}

function makeThrowingLocalStorageMock() {
  return {
    getItem: () => null,
    setItem: vi.fn(() => { throw new DOMException('QuotaExceeded', 'QuotaExceededError'); }),
    removeItem: () => {},
    clear: () => {},
  };
}

// ---------------------------------------------------------------------------
// Notification mock helpers
// ---------------------------------------------------------------------------

function mockNotification({ permission = 'default', requestResult = 'granted', requestThrows = false } = {}) {
  let _perm = permission;
  const ctor = vi.fn();
  Object.defineProperty(ctor, 'permission', {
    get: () => _perm,
    set: (v) => { _perm = v; },
    configurable: true,
  });
  ctor.requestPermission = async () => {
    if (requestThrows) throw new Error('blocked');
    const r = requestResult;
    if (r === 'granted' || r === 'denied') _perm = r;
    return r;
  };
  global.Notification = ctor;
}

// ---------------------------------------------------------------------------
// notifEnable
// ---------------------------------------------------------------------------

describe('notifEnable', () => {
  let dispatchSpy;
  let savedLS;

  beforeEach(() => {
    dispatchSpy = vi.spyOn(window, 'dispatchEvent').mockImplementation(() => true);
    savedLS = Object.getOwnPropertyDescriptor(window, 'localStorage');
    delete global.Notification;
  });

  afterEach(() => {
    vi.restoreAllMocks();
    if (savedLS) {
      Object.defineProperty(window, 'localStorage', savedLS);
    } else {
      Object.defineProperty(window, 'localStorage', { value: undefined, writable: true, configurable: true });
    }
    delete global.Notification;
  });

  function installLS(mock) {
    Object.defineProperty(window, 'localStorage', { value: mock, writable: true, configurable: true });
  }

  it('returns "off" when Notification API is unavailable', async () => {
    installLS(makeLocalStorageMock());
    delete global.Notification;
    const result = await notifEnable();
    expect(result).toBe('off');
    expect(dispatchSpy).not.toHaveBeenCalled();
  });

  it('returns "denied" immediately when permission is already denied', async () => {
    installLS(makeLocalStorageMock());
    mockNotification({ permission: 'denied' });
    const result = await notifEnable();
    expect(result).toBe('denied');
    expect(dispatchSpy).not.toHaveBeenCalled();
  });

  it('returns "on" and dispatches {detail:true} when already granted + LS write succeeds', async () => {
    const ls = makeLocalStorageMock();
    installLS(ls);
    mockNotification({ permission: 'granted' });
    const result = await notifEnable();
    expect(result).toBe('on');
    expect(ls.setItem).toHaveBeenCalledWith('viewer.notifications.enabled', 'true');
    expect(dispatchSpy).toHaveBeenCalledOnce();
    const evt = dispatchSpy.mock.calls[0][0];
    expect(evt.type).toBe('notifEnabledSync');
    expect(evt.detail).toBe(true);
  });

  it('returns "storage-failed" and dispatches {detail:false} when LS write throws', async () => {
    installLS(makeThrowingLocalStorageMock());
    mockNotification({ permission: 'granted' });
    const result = await notifEnable();
    expect(result).toBe('storage-failed');
    expect(dispatchSpy).toHaveBeenCalledOnce();
    expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
  });

  it('requests permission when "default", returns "on" on grant', async () => {
    const ls = makeLocalStorageMock();
    installLS(ls);
    mockNotification({ permission: 'default', requestResult: 'granted' });
    const result = await notifEnable();
    expect(result).toBe('on');
    expect(dispatchSpy).toHaveBeenCalledOnce();
    expect(dispatchSpy.mock.calls[0][0].detail).toBe(true);
  });

  it('returns "denied" when requestPermission resolves "denied"', async () => {
    installLS(makeLocalStorageMock());
    mockNotification({ permission: 'default', requestResult: 'denied' });
    const result = await notifEnable();
    expect(result).toBe('denied');
    expect(dispatchSpy).not.toHaveBeenCalled();
  });

  it('returns "off" when requestPermission resolves "dismissed"', async () => {
    installLS(makeLocalStorageMock());
    mockNotification({ permission: 'default', requestResult: 'dismissed' });
    const result = await notifEnable();
    expect(result).toBe('off');
    expect(dispatchSpy).not.toHaveBeenCalled();
  });

  it('returns "off" when requestPermission throws', async () => {
    installLS(makeLocalStorageMock());
    mockNotification({ permission: 'default', requestThrows: true });
    const result = await notifEnable();
    expect(result).toBe('off');
    expect(dispatchSpy).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// notifDisable
// ---------------------------------------------------------------------------

describe('notifDisable', () => {
  let dispatchSpy;
  let savedLS;

  beforeEach(() => {
    dispatchSpy = vi.spyOn(window, 'dispatchEvent').mockImplementation(() => true);
    savedLS = Object.getOwnPropertyDescriptor(window, 'localStorage');
  });

  afterEach(() => {
    vi.restoreAllMocks();
    if (savedLS) {
      Object.defineProperty(window, 'localStorage', savedLS);
    } else {
      Object.defineProperty(window, 'localStorage', { value: undefined, writable: true, configurable: true });
    }
  });

  function installLS(mock) {
    Object.defineProperty(window, 'localStorage', { value: mock, writable: true, configurable: true });
  }

  it('writes "false" to localStorage and dispatches {detail:false}', () => {
    const ls = makeLocalStorageMock({ 'viewer.notifications.enabled': 'true' });
    installLS(ls);
    notifDisable();
    expect(ls.setItem).toHaveBeenCalledWith('viewer.notifications.enabled', 'false');
    expect(dispatchSpy).toHaveBeenCalledOnce();
    const evt = dispatchSpy.mock.calls[0][0];
    expect(evt.type).toBe('notifEnabledSync');
    expect(evt.detail).toBe(false);
  });

  it('does not throw when localStorage throws', () => {
    installLS(makeThrowingLocalStorageMock());
    dispatchSpy.mockImplementation(() => { throw new Error('csp'); });
    expect(() => notifDisable()).not.toThrow();
  });
});
