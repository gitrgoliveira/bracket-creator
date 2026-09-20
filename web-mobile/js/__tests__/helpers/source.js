// Read a web-mobile module's source with comments stripped.
//
// The absence assertions in this suite ("no surface renders X any more") have a
// trap: every removal site explains ITSELF in a comment, so a check that read
// the raw file matches its own explanation and fails. That bit twice while the
// bc-sccl badge removals were written, once per test file, which is why the
// stripping lives here rather than being re-typed per file.
//
// Block comments go first; then whole-line // comments, matched from the line
// start so a "https://" inside code is never mistaken for one.
import { readFileSync } from 'fs';
import { dirname, resolve } from 'path';
import { fileURLToPath } from 'url';

const JS_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');

/** Raw source of a module in web-mobile/js. */
export const readSource = (file) => readFileSync(resolve(JS_DIR, file), 'utf8');

/** Source with comments removed, for "this is not rendered any more" checks. */
export const readCode = (file) => readSource(file)
  .replace(/\/\*[\s\S]*?\*\//g, '')
  .replace(/^\s*\/\/.*$/gm, '');
