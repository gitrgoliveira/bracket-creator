// T3 regression guard: the Tie and Fusensho action buttons in
// admin_scoring_team.jsx must NOT embed TermAS children. TermAS
// stopPropagation()s click events so tapping the word opens the glossary
// tooltip instead of recording the score.
//
// TeamScoreEditorModal cannot be mounted in vitest (the hook stubs only
// support initial renders; the modal's large state tree needs full interaction
// to reach the button). A source-text guard is the right tool here: it pins
// the structural constraint across future edits at near-zero cost.
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __filename = fileURLToPath(import.meta.url);
const __dir = dirname(__filename);
const src = readFileSync(join(__dir, '../admin_scoring_team.jsx'), 'utf8');

// Extract the JSX text belonging to each named button via a minimal bracket
// scan rather than a regex that could match the wrong closing bracket.
function extractButtonContent(source, testId) {
  const open = `data-testid="${testId}"`;
  const start = source.indexOf(open);
  if (start === -1) return null;
  // Walk forward from the opening tag to find its closing </button>.
  let depth = 0;
  let _inContent = false; void _inContent;
  let i = start;
  // Go back to find the '<button' that precedes the testId attribute.
  const btnStart = source.lastIndexOf('<button', start);
  i = btnStart;
  const end = source.length;
  while (i < end) {
    if (source.startsWith('<button', i)) { depth++; i += 7; continue; }
    if (source.startsWith('</button>', i)) {
      depth--;
      if (depth === 0) return source.slice(btnStart, i + 9);
      i += 9;
      continue;
    }
    i++;
  }
  return null;
}

describe('T3: action buttons must not embed TermAS (stopPropagation safety)', () => {
  it('scoring-modal-tie-button text content is plain text (Tie (hikiwake))', () => {
    const chunk = extractButtonContent(src, 'scoring-modal-tie-button');
    expect(chunk).not.toBeNull();
    // Must contain the plain text label.
    expect(chunk).toContain('Tie (hikiwake)');
    // Must NOT contain TermAS (would swallow taps via stopPropagation).
    expect(chunk).not.toContain('TermAS');
    // Must NOT render a data-testid="term-wrapper" child.
    expect(chunk).not.toContain('term-wrapper');
  });

  it('scoring-modal-fusensho-button text content is plain text (Fusensho)', () => {
    const chunk = extractButtonContent(src, 'scoring-modal-fusensho-button');
    expect(chunk).not.toBeNull();
    // Must contain the plain text label.
    expect(chunk).toContain('Fusensho');
    // Must NOT contain TermAS.
    expect(chunk).not.toContain('TermAS');
    expect(chunk).not.toContain('term-wrapper');
  });
});
