// mp-xhaa: unit tests for notifEnable/notifDisable helpers.
// These pure helpers drive AnnBellBtn, NotificationSettings, and the
// NOTIF_SYNC_EVENT broadcast — cover every return value branch.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { notifEnable, notifDisable } from '../viewer.jsx';
import { makeLocalStorageMock, makeThrowingLocalStorageMock } from './test_helpers.js';

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

function installLS(mock) {
  Object.defineProperty(window, 'localStorage', { value: mock, writable: true, configurable: true });
}

// ---------------------------------------------------------------------------
// Tests — shared setup/teardown in parent describe
// ---------------------------------------------------------------------------

describe('notification helpers', () => {
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

  describe('notifEnable', () => {
    it('returns "off" when Notification API is unavailable', async () => {
      installLS(makeLocalStorageMock());
      delete global.Notification;
      const result = await notifEnable();
      expect(result).toBe('off');
      expect(dispatchSpy).not.toHaveBeenCalled();
    });

    it('returns "denied" and dispatches {detail:false} when permission is already denied', async () => {
      installLS(makeLocalStorageMock());
      mockNotification({ permission: 'denied' });
      const result = await notifEnable();
      expect(result).toBe('denied');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
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

    it('returns "off" and dispatches {detail:false} when LS write throws', async () => {
      installLS(makeThrowingLocalStorageMock());
      mockNotification({ permission: 'granted' });
      const result = await notifEnable();
      expect(result).toBe('off');
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

    it('returns "denied" and dispatches {detail:false} when requestPermission resolves "denied"', async () => {
      installLS(makeLocalStorageMock());
      mockNotification({ permission: 'default', requestResult: 'denied' });
      const result = await notifEnable();
      expect(result).toBe('denied');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
    });

    it('returns "off" and dispatches {detail:false} when requestPermission resolves "dismissed"', async () => {
      installLS(makeLocalStorageMock());
      mockNotification({ permission: 'default', requestResult: 'dismissed' });
      const result = await notifEnable();
      expect(result).toBe('off');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
    });

    it('returns "off" when requestPermission throws', async () => {
      installLS(makeLocalStorageMock());
      mockNotification({ permission: 'default', requestThrows: true });
      const result = await notifEnable();
      expect(result).toBe('off');
      expect(dispatchSpy).not.toHaveBeenCalled();
    });
  });

  describe('notifDisable', () => {
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
});
