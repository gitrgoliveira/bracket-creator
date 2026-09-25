// bc-cse (fix #4): teamNameMark/teamNameMarkStr (match_scoreboard.jsx) and
// barredNameMark (barred_chip.jsx) wrap a NumberedName clip element inside a
// BLOCK-ELLIPSIS cell (VSchedItem's .n, TWMatch's .tw-match__name, the admin
// Scores row's .name, the TV headline -- see the sibling render suite for the
// latter two). A long Shiro name used to clip the trailing mark/chip away;
// on Aka, whose mark leads, it pushed the mark past the visible width.
//
// jsdom cannot measure the actual clip (see vsched_number_survives_truncation
// .test.jsx, which pins the identical class of bug for the competitor number
// chip). What a regression here actually undoes is the STRUCTURE this file
// pins: the cell becomes a flex row (msb-name--labelled) so only the name
// shrinks/ellipsises, and the mark is a real, flex:none SIBLING element --
// never text baked into the same run.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findAll, hasClass } from './helpers/vdom.js';
import { readStylesheet, cssBlock } from './helpers/source.js';

const realReact = global.React;

describe('a team-match name mark survives clipping (bc-cse #4)', () => {
  describe('CSS: .msb-name--labelled supplies the spacing and the flex sizing', () => {
    const css = readStylesheet();

    it('is a flex row with a gap -- not the {" "} text-node spacer teamNameMark/barredNameMark still emit, which CSS Flexbox treats as ZERO-SIZED between flex items', () => {
      const block = cssBlock(css, '.msb-name--labelled');
      expect(block).toMatch(/display:\s*flex/);
      expect(block).toMatch(/gap:\s*[^;]+;/);
    });

    it('the mark/chip classes are flex:none so they never shrink', () => {
      const block = cssBlock(css, '.msb-name--labelled .msb-member-label,\n.msb-name--labelled .sb-result-mark,\n.msb-name--labelled .barred-chip')
        || cssBlock(css, '.msb-name--labelled .sb-result-mark');
      expect(block).toMatch(/flex-shrink:\s*0/);
    });

    it('the NumberedName clip wrapper can actually shrink inside the row (min-width: 0)', () => {
      const block = cssBlock(css, '.msb-name--labelled .numbered-name--clip');
      expect(block).toMatch(/min-width:\s*0/);
    });
  });

  describe('VSchedItem (viewer_match.jsx): .n becomes a flex row', () => {
    let runtime, VSchedItem;

    beforeEach(async () => {
      runtime = makeReactive();
      global.React = runtime.React;
      global.window = global.window || {};
      global.window.matchScoreStr = vi.fn(() => '');
      global.window.boutMiddle = vi.fn(() => 'vs');
      global.window.queueLabelCompact = null;
      global.window.teamMatchMarks = vi.fn(() => ({ shiro: 'Kiken', aka: '' }));
      vi.resetModules();
      ({ VSchedItem } = await import('../viewer_match.jsx'));
    });

    afterEach(() => {
      runtime.unmount();
      global.React = realReact;
      delete global.window.matchScoreStr;
      delete global.window.boutMiddle;
      delete global.window.queueLabelCompact;
      delete global.window.teamMatchMarks;
      vi.restoreAllMocks();
      vi.resetModules();
    });

    it('both .n cells carry msb-name--labelled, and the mark is a real sb-result-mark sibling, not joined into the name string', () => {
      const m = {
        id: 'm1', status: 'completed', court: 'A', decision: 'kiken-voluntary',
        subResults: [{ position: 1 }],
        sideA: { id: 'p-aka', name: 'AOKI TARO' },
        sideB: { id: 'p-shiro', name: 'A VERY LONG SHIRO COMPETITOR NAME' },
      };
      const tree = runtime.mount(VSchedItem, { m, tweaks: {} });

      const cells = findAll(tree, n => hasClass(n, 'n'));
      expect(cells).toHaveLength(2);
      for (const cell of cells) expect(hasClass(cell, 'msb-name--labelled')).toBe(true);

      // Shiro carries the Kiken mark (bc-tmfn convention: shiro=sideB wins).
      const marks = findAll(tree, n => hasClass(n, 'sb-result-mark'));
      expect(marks).toHaveLength(1);
      expect(marks[0].props.children).toBe('Kiken');

      // The mark is a SIBLING, never inside the name prop the ellipsis targets.
      const clipNames = findAll(tree, n => n.props && n.props.clip === true);
      expect(clipNames.length).toBeGreaterThan(0);
      for (const n of clipNames) {
        expect(typeof n.props.name).toBe('string');
        expect(n.props.name).not.toContain('Kiken');
      }
    });
  });

  describe('TWMatch (viewer_schedule.jsx): .tw-match__name becomes a flex row, the mark its own element', () => {
    let runtime, TWMatch;

    beforeEach(async () => {
      runtime = makeReactive();
      global.React = runtime.React;
      global.window = global.window || {};
      global.window.matchScoreStr = vi.fn(() => '');
      global.window.queueLabelCompact = null;
      global.window.teamMatchMarks = vi.fn(() => ({ shiro: '', aka: 'Fus.' }));
      vi.resetModules();
      ({ TWMatch } = await import('../viewer_schedule.jsx'));
    });

    afterEach(() => {
      runtime.unmount();
      global.React = realReact;
      delete global.window.matchScoreStr;
      delete global.window.queueLabelCompact;
      delete global.window.teamMatchMarks;
      vi.restoreAllMocks();
      vi.resetModules();
    });

    it('carries msb-name--labelled, and the mark is a real sb-result-mark sibling of an msb-name__text span -- not appended to the string', () => {
      const m = {
        id: 'm2', status: 'completed', court: 'A', decision: 'fusenpai', decisionBy: 'aka',
        subResults: [{ position: 1 }],
        sideA: { id: 'p-aka', name: 'A VERY LONG AKA COMPETITOR NAME', number: 'K1' },
        sideB: { id: 'p-shiro', name: 'ITO KEN', number: 'K2' },
      };
      const tree = runtime.mount(TWMatch, { m });

      const cells = findAll(tree, n => hasClass(n, 'tw-match__name'));
      expect(cells.length).toBeGreaterThanOrEqual(2);
      for (const cell of cells) expect(hasClass(cell, 'msb-name--labelled')).toBe(true);

      // Aka carries the Fus. mark. It must be its own element (this is the
      // regression fix #4 closes for THIS host specifically: teamNameMarkStr
      // used to bake it into the plain string).
      const marks = findAll(tree, n => hasClass(n, 'sb-result-mark'));
      expect(marks).toHaveLength(1);
      expect(marks[0].props.children).toBe('Fus.');

      // The name+number STRING sits in its own msb-name__text sibling, so it
      // alone ellipsises; the mark never shares that text run.
      const textEls = findAll(tree, n => hasClass(n, 'msb-name__text'));
      expect(textEls.length).toBeGreaterThan(0);
      const akaText = textEls.find(n => String(n.children || n.props?.children).includes('AKA COMPETITOR'));
      expect(akaText).toBeTruthy();
      expect(String(akaText.children || akaText.props?.children)).not.toContain('Fus.');
    });
  });
});
