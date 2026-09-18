// Shared vnode-tree walkers for match_scoreboard tests: TeamScoreboard renders
// BoutSubRow children without expanding, so boutRows collects their vnode props.

export function boutRows(node, out = []) {
  if (!node || typeof node !== 'object') return out;
  if (Array.isArray(node)) { node.forEach(n => boutRows(n, out)); return out; }
  const p = node.props || {};
  if (p.sub !== undefined && typeof p.index === 'number') out.push(p);
  boutRows(node.children || p.children, out);
  return out;
}

export function findInTree(node, pred) {
  if (!node || typeof node !== 'object') return null;
  if (Array.isArray(node)) { for (const k of node) { const f = findInTree(k, pred); if (f) return f; } return null; }
  if (pred(node)) return node;
  const kids = node.children || node.props?.children || [];
  for (const k of [].concat(kids)) { const f = findInTree(k, pred); if (f) return f; }
  return null;
}

// collectText: every string/number leaf of a vnode tree, concatenated.
//
// `expand` is OPTIONAL and additive: omit it and this behaves exactly as it
// always has. Pass a predicate over a vnode's `type` and a matching FUNCTION
// component is invoked so its rendered text is reached too.
//
// That option exists because the mock runtime's createElement never invokes a
// function `type` (see reactive_react.js), so text moved into a component
// disappears from a plain text sweep. Test files had each answered that
// privately, and the copies drifted into five different policies. The policy is
// a real per-file decision -- expanding by NAME survives vi.resetModules()
// where identity does not, and a file whose assertions match components by
// identity must NOT expand them -- so the policy stays with the caller and only
// the walk lives here.
//
// The try/catch is not defensive padding: components that read context or hooks
// throw when called outside the runtime's own bookkeeping, and two suites
// expand broadly enough to hit that. Falling through to the children is the
// same answer not expanding would have given.
export function collectText(node, expand) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(n => collectText(n, expand)).join('');
  if (expand && typeof node.type === 'function' && expand(node.type)) {
    try {
      const props = { ...node.props };
      if (node.children?.length) {
        props.children = node.children.length === 1 ? node.children[0] : node.children;
      }
      return collectText(node.type(props), expand);
    } catch { /* not callable bare: fall through to its children */ }
  }
  if (node.children) return collectText(node.children, expand);
  if (node.props?.children) return collectText(node.props.children, expand);
  return '';
}

// Ready-made policies for collectText's `expand`, so the two shapes that
// recur are named once rather than re-spelled per file.
export const expandAll = () => true;
export const expandNamed = (...names) => (type) => names.includes(type.name);

// NOT hoisted here, deliberately: findHosts. Its three copies are identical to
// each other but are NOT self-contained -- each delegates to a local `walk`,
// which in turn calls a local `childrenOf`, and `walk` alone is redefined in
// eight files with its own boolean-filtering rules. Lifting findHosts without
// that chain would mean writing a fresh implementation and silently changing
// what three suites match, so the chain wants its own pass rather than a
// drive-by extraction.

// findAll: collect-all variant of findInTree (every node matching pred, not
// just the first), depth-first over the same vnode shape.
export function findAll(node, pred, out = []) {
  if (!node || typeof node !== 'object') return out;
  if (Array.isArray(node)) { node.forEach(n => findAll(n, pred, out)); return out; }
  if (pred(node)) out.push(node);
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach(k => findAll(k, pred, out));
  return out;
}

// hasClass: true when a vnode's className prop contains cls as a token.
export function hasClass(node, cls) {
  return String(node?.props?.className || '').split(' ').includes(cls);
}
