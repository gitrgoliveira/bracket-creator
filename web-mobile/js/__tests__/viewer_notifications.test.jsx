// mp-xhaa: unit tests for notifEnable/notifDisable helpers.
// These pure helpers drive AnnBellBtn and the
// NOTIF_SYNC_EVENT broadcast — cover every return value branch.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { notifEnable, notifDisable, NOTIF_SYNC_EVENT } from '../viewer.jsx';
import { LS_NOTIFICATIONS_ENABLED } from '../notification_keys.jsx';
import { makeLocalStorageMock, makeThrowingLocalStorageMock, makeFullyLockedLocalStorageMock, makeNotifMock } from './test_helpers.js';

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
      expect(ls.setItem).toHaveBeenCalledWith(LS_NOTIFICATIONS_ENABLED, 'true');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      const evt = dispatchSpy.mock.calls[0][0];
      expect(evt.type).toBe(NOTIF_SYNC_EVENT);
      expect(evt.detail).toBe(true);
    });

    it('returns "off" and dispatches {detail:false} when LS write throws and key was absent', async () => {
      installLS(makeThrowingLocalStorageMock());
      global.Notification = makeNotifMock({ permission: 'granted' });
      const result = await notifEnable();
      expect(result).toBe('off');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(false);
    });

    it('returns "on" and dispatches {detail:true} when LS write throws but key was already "true"', async () => {
      // setItem throws (quota), but the key was already persisted "true" from a
      // previous opt-in. fireNotification() will read "true", so bells must show "on".
      const ls = makeLocalStorageMock({ [LS_NOTIFICATIONS_ENABLED]: 'true' });
      ls.setItem.mockImplementation(() => { throw new DOMException('QuotaExceeded', 'QuotaExceededError'); });
      installLS(ls);
      global.Notification = makeNotifMock({ permission: 'granted' });
      const result = await notifEnable();
      expect(result).toBe('on');
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(true);
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

    it('returns "off" when notifDisable() is called while awaiting permission', async () => {
      const ls = makeLocalStorageMock();
      installLS(ls);
      // Deferred permission so we can interleave notifDisable() before it resolves.
      let resolvePermission;
      const permPromise = new Promise((res) => { resolvePermission = res; });
      global.Notification = { permission: 'default', requestPermission: () => permPromise };

      const enableResult = notifEnable();
      notifDisable(); // sets notifCancelled = true while dialog is pending
      resolvePermission('granted'); // user approves — but already cancelled

      const result = await enableResult;
      expect(result).toBe('off');
      // LS must NOT have been written with 'true' (the disable ran first)
      expect(ls.setItem).not.toHaveBeenCalledWith(LS_NOTIFICATIONS_ENABLED, 'true');
      expect(dispatchSpy.mock.calls.at(-1)[0].detail).toBe(false);
    });
  });

  describe('notifDisable', () => {
    it('writes "false" to localStorage and dispatches {detail:false}', () => {
      const ls = makeLocalStorageMock({ [LS_NOTIFICATIONS_ENABLED]: 'true' });
      installLS(ls);
      notifDisable();
      expect(ls.setItem).toHaveBeenCalledWith(LS_NOTIFICATIONS_ENABLED, 'false');
      expect(ls.getItem).toHaveBeenCalledWith(LS_NOTIFICATIONS_ENABLED);
      expect(dispatchSpy).toHaveBeenCalledOnce();
      const evt = dispatchSpy.mock.calls[0][0];
      expect(evt.type).toBe(NOTIF_SYNC_EVENT);
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
      const ls = makeFullyLockedLocalStorageMock({ [LS_NOTIFICATIONS_ENABLED]: 'true' });
      installLS(ls);
      notifDisable();
      expect(ls.removeItem).toHaveBeenCalledWith(LS_NOTIFICATIONS_ENABLED);
      expect(ls.getItem).toHaveBeenCalledWith(LS_NOTIFICATIONS_ENABLED);
      expect(dispatchSpy).toHaveBeenCalledOnce();
      expect(dispatchSpy.mock.calls[0][0].detail).toBe(true);
    });
  });
});
