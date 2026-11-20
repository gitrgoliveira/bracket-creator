// blob_download.jsx: the ONE owner of handing a downloaded Blob to the browser
// as a file (the workbooks, the PDF zip, the CSV template, a backup).
//
// The revoke waits a moment so the browser starts the download before the blob
// URL is torn down, and it is guarded against a torn-down context: in a real
// browser window is always present, but in unit-test teardown this
// fire-and-forget timer can outlive the jsdom window and would otherwise throw
// uncaught. Create and revoke both go through window.URL, which the unit tests
// mock.
//
// A leaf with no imports: every consumer ES-imports it directly.
const REVOKE_DELAY_MS = 100;

export function downloadBlob(blob, filename) {
  const url = window.URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => { if (typeof window !== "undefined" && window.URL) window.URL.revokeObjectURL(url); }, REVOKE_DELAY_MS);
}
