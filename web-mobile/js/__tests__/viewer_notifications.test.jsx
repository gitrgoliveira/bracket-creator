// mp-xhaa: unit tests for notifEnable/notifDisable helpers.
// These pure helpers drive AnnBellBtn, NotificationSettings, and the
// NOTIF_SYNC_EVENT broadcast — cover every return value branch.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { notifEnable, notifDisable } from '../viewer.jsx';
import { makeLocalStorageMock, makeThrowingLocalStorageMock, makeNotifMock } from './test_helpers.js';

function installLS(mock) {
  Object.defineProperty(window, 'localStorage', { value: mock, writable: true, configurable: true });
}

// ---------------------------------------------------------------------------
// Tests — shared setup/teardown in parent describe
// ---------------------------------------------------------------------------

describe('notification helpers', () => {
  let dispatchSpy;
  let savedLS;
  let savedNotification;

  beforeEach(() => {
    savedNotification = global.Notification;
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
    if (savedNotification !== undefined) {
      global.Notification = savedNotification;
    } else {
      delete global.Notification;
    }
  });

  describe('notifEnable', () => {
    it('returns "off" when Notification API is unavailable', async () => {
      installLS(makeLocalStorageMock());
      delete global.Notification;
      const result = await notifEnable();
      expect(result).toBe('off');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
    });

    it('returns "denied" and dispatches {detail:false} when permission is already denied', async () => {
      installLS(makeLocalStorageMock());
      global.Notification = makeNotifMock({ permission: 'denied' });
      const result = await notifEnable();
      expect(result).toBe('denied');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
    });

    it('returns "on" and dispatches {detail:true} when already granted + LS write succeeds', async () => {
      const ls = makeLocalStorageMock();
      installLS(ls);
      global.Notification = makeNotifMock({ permission: 'granted' });
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
      global.Notification = makeNotifMock({ permission: 'granted' });
      const result = await notifEnable();
      expect(result).toBe('off');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
    });

    it('requests permission when "default", returns "on" on grant', async () => {
      const ls = makeLocalStorageMock();
      installLS(ls);
      global.Notification = makeNotifMock({ permission: 'default', requestResult: 'granted' });
      const result = await notifEnable();
      expect(result).toBe('on');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(true);
    });

    it('returns "denied" and dispatches {detail:false} when requestPermission resolves "denied"', async () => {
      installLS(makeLocalStorageMock());
      global.Notification = makeNotifMock({ permission: 'default', requestResult: 'denied' });
      const result = await notifEnable();
      expect(result).toBe('denied');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
    });

    it('returns "off" and dispatches {detail:false} when requestPermission resolves "default" (user dismissed)', async () => {
      installLS(makeLocalStorageMock());
      global.Notification = makeNotifMock({ permission: 'default', requestResult: 'default' });
      const result = await notifEnable();
      expect(result).toBe('off');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
    });

    it('returns "off" and dispatches {detail:false} when requestPermission throws', async () => {
      installLS(makeLocalStorageMock());
      global.Notification = makeNotifMock({ permission: 'default', requestThrows: true });
      const result = await notifEnable();
      expect(result).toBe('off');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
    });
  });

  describe('notifDisable', () => {
    it('writes "false" to localStorage and dispatches {detail:false}', () => {
      const ls = makeLocalStorageMock({ 'viewer.notifications.enabled': 'true' });
      installLS(ls);
      notifDisable();
      expect(ls.setItem).toHaveBeenCalledWith('viewer.notifications.enabled', 'false');
      expect(ls.getItem).toHaveBeenCalledWith('viewer.notifications.enabled');
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

    it('dispatches {detail:true} when both setItem and removeItem fail and key remains "true"', () => {
      // Storage completely locked (e.g. corrupted profile): neither setItem nor
      // removeItem can write. The read-back reflects the actual persisted state
      // so all bell instances stay in sync with what fireNotification() will see.
      const store = { 'viewer.notifications.enabled': 'true' };
      const ls = {
        getItem: vi.fn((k) => (k in store ? store[k] : null)),
        setItem: vi.fn(() => { throw new DOMException('QuotaExceeded', 'QuotaExceededError'); }),
        removeItem: vi.fn(() => { throw new DOMException('QuotaExceeded', 'QuotaExceededError'); }),
        clear: () => {},
      };
      installLS(ls);
      notifDisable();
      expect(ls.removeItem).toHaveBeenCalledWith('viewer.notifications.enabled');
      expect(ls.getItem).toHaveBeenCalledWith('viewer.notifications.enabled');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(true);
    });
  });
});
