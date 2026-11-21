// Setup for the `node` project: unit files that touch no page.
// Only the console guard. No fake React, no `window.*` side-effect imports and
// no storage shim: a file that needs any of them belongs in the jsdom unit project.
import './vitest.setup.console.js';
