import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { downloadBlob } from '../blob_download.jsx';

describe('downloadBlob', () => {
  let saved;
  beforeEach(() => {
    vi.useFakeTimers();
    saved = { create: window.URL.createObjectURL, revoke: window.URL.revokeObjectURL };
    window.URL.createObjectURL = vi.fn(() => 'blob:test-url');
    window.URL.revokeObjectURL = vi.fn();
  });
  afterEach(() => {
    vi.useRealTimers();
    window.URL.createObjectURL = saved.create;
    window.URL.revokeObjectURL = saved.revoke;
    document.body.innerHTML = '';
  });

  it('clicks a download link named for the file, then removes it', () => {
    const clicked = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function () {
      clicked.push({ href: this.href, download: this.download, attached: document.body.contains(this) });
    });
    downloadBlob(new Blob(['x']), 'out.xlsx');
    expect(clicked).toEqual([{ href: 'blob:test-url', download: 'out.xlsx', attached: true }]);
    expect(document.querySelector('a')).toBeNull();
    click.mockRestore();
  });

  it('revokes the URL only after the download has had time to start', () => {
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    downloadBlob(new Blob(['x']), 'out.xlsx');
    expect(window.URL.revokeObjectURL).not.toHaveBeenCalled();
    vi.advanceTimersByTime(100);
    expect(window.URL.revokeObjectURL).toHaveBeenCalledWith('blob:test-url');
    click.mockRestore();
  });
});
