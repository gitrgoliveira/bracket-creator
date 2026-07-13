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

export function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

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
