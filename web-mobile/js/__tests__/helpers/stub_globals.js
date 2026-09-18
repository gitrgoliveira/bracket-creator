// stub_globals.js: install a test file's window.* stubs and get back the
// function that puts the real ones back (bc-rvfx).
//
// Thirty-six test files carried their own copy of this: a STUBBED_GLOBALS
// object, a `const originals = {}`, a beforeAll loop that saved-then-replaced,
// and an afterAll loop that restored. The stub SETS differ legitimately from
// file to file (7 to 25 keys, depending on what the module under test reaches
// for), but the save/restore MECHANISM was identical in every one -- so the
// mechanism is what moves here and the set stays with the file that needs it.
//
// The subtlety worth centralising is the `had` flag. Restoring must
// distinguish "this global existed and had value X" from "this global did not
// exist at all": assigning `undefined` back is NOT the same as deleting the
// key, because `k in window` stays true afterwards and the next file's own
// `had` check then reads the wrong answer. Vitest shares one jsdom window
// across the files in a worker, so a file that restores sloppily leaks into
// its neighbours, and the symptom is a failure in an unrelated test that only
// appears in a particular file ORDER. One correct implementation removes that
// whole class.
//
// Usage:
//   let restore;
//   beforeAll(async () => { restore = installWindowStubs(STUBBED_GLOBALS); ... });
//   afterAll(() => restore());
export function installWindowStubs(stubs) {
  const originals = {};
  for (const [key, value] of Object.entries(stubs)) {
    originals[key] = { had: key in window, value: window[key] };
    window[key] = value;
  }
  return function restoreWindowStubs() {
    for (const [key, original] of Object.entries(originals)) {
      if (original.had) window[key] = original.value;
      else delete window[key];
    }
  };
}
