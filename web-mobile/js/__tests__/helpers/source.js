// Read a web-mobile module's source with comments stripped.
//
// The absence assertions in this suite ("no surface renders X any more") have a
// trap: every removal site explains ITSELF in a comment, so a check that read
// the raw file matches its own explanation and fails. That bit twice while the
// bc-sccl badge removals were written, once per test file, which is why the
// stripping lives here rather than being re-typed per file.
//
// stripComments is ONE alternation pass, line comments FIRST in the
// alternation. Stripping block comments in a separate earlier pass was a real
// blind spot: a "/*" inside a // comment (a path like /api/competitions/:id/*,
// a glob like *.jsx) opened a phantom block that swallowed source to the next
// "*/". match_scoreboard.jsx kept 8,355 of its 51,347 bytes that way, so the
// repo-wide sweeps below read almost none of it. Pinned by retentionRatio.
// competition_parity.test.jsx carried an independent copy of this same
// block-first bug; it now imports stripComments from here instead.
//
// Line comments are still matched from the line START, so a "https://" in
// code is never mistaken for one. The flip side, stated rather than papered
// over: a TRAILING // comment is not stripped, so one containing "/*" can
// still open a phantom block. No module in the tree does that today and
// retentionRatio is what would catch it.
import { readFileSync, readdirSync } from 'fs';
import { dirname, resolve } from 'path';
import { fileURLToPath } from 'url';

const JS_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');

/** Raw source of a module in web-mobile/js. */
export const readSource = (file) => readFileSync(resolve(JS_DIR, file), 'utf8');

/** Comments stripped from a source string. See the file header for why this
 * is one alternation pass rather than two sequential ones. */
export const stripComments = (src) => src.replace(/^[ \t]*\/\/.*$|\/\*[\s\S]*?\*\//gm, '');

/** Source of a module in web-mobile/js with comments removed, for "this is
 * not rendered any more" checks. */
export const readCode = (file) => stripComments(readSource(file));

/** Share of a module's bytes that survive readCode. Guards the phantom-block bug. */
export const retentionRatio = (file) => readCode(file).length / readSource(file).length;

/** Read the stylesheet once; callers pass it to cssBlock. */
export const readStylesheet = () =>
  readFileSync(resolve(JS_DIR, '..', 'css', 'styles.css'), 'utf8');

// The declarations of one top-level CSS rule, found by ANY selector in its
// list. Two suites had a copy of this that looked for `\n<selector> {`, so a
// rule the selector merely SHARES -- `.side-fill--shiro,\n.pool-...--shiro {`
// -- stopped being found, and the failure read as "the fill is gone" when only
// its spelling had changed. Matches the selector at a line start followed by
// either `{` (alone) or `,` (one entry of a list).
export const cssBlock = (css, selector) => {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const m = new RegExp(`^${esc}\\s*[,{]`, 'm').exec(css);
  if (!m) return null;
  return css.slice(m.index, css.indexOf('}', css.indexOf('{', m.index)));
};

/** Every top-level module in web-mobile/js, for repo-wide sweeps. */
export const modules = () => readdirSync(JS_DIR).filter((f) => f.endsWith('.jsx'));

// A word as it would REACH a reader: a quoted literal, or a JSX text node.
// Three suites spelled this by hand. The `>word<` arm is not decoration -- it
// was added after a mutation that re-added `<span>HANTEI</span>` walked past a
// literal-only check, so a sweep that omits it passes on the very edit it
// exists to catch. One owner means the next widening reaches every caller.
export const renderedLiteral = (word) =>
  new RegExp(`(["'\`](?:${word})["'\`]|>\\s*(?:${word})\\s*<)`);
