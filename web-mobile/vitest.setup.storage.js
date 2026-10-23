// Vitest 5's jsdom environment exposes the window's storage objects on the
// global as getter-only accessors, so a test's `global.localStorage = stub`
// throws "Cannot set property localStorage ... which has only a getter".
// Vitest 4 copied them as plain writable properties, and the suites swap a
// stub in that way, so turn each back into a writable data property holding
// the same jsdom object. Shared by vitest.setup.js and vitest.setup.render.js.
for (const name of ['localStorage', 'sessionStorage']) {
  Object.defineProperty(globalThis, name, {
    value: globalThis[name],
    writable: true,
    configurable: true,
    enumerable: true,
  });
}
