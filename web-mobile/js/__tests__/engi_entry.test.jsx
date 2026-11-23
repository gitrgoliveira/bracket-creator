import { describe, it, expect } from 'vitest';
import {
  engiEntryProblem, engiShape, engiNameFieldLabel,
  ENGI_PAIR_PROBLEM, ENGI_SOLO_PROBLEM, ENGI_SOLO_BLANK,
} from '../engi_entry.jsx';

// engi_entry.jsx is the one owner of "does this name fit an engi side". A pair
// competition (engiSolo off) takes "Name 1 - Name 2"; a solo one (engiSolo on)
// takes one competitor and refuses a pair with a sentence that says how to
// enter pairs instead.

describe('engi entry sentences', () => {
  it('say what to do, word for word', () => {
    expect(ENGI_PAIR_PROBLEM).toBe('Enter both names of the pair as "Name 1 - Name 2".');
    expect(ENGI_SOLO_PROBLEM).toBe('This competition takes one competitor per side. To enter pairs, turn off "One competitor per side" in Settings.');
    expect(ENGI_SOLO_BLANK).toBe("Enter the competitor's name.");
  });
});

describe('engiEntryProblem, pair', () => {
  it('accepts a pair with both members present', () => {
    expect(engiEntryProblem('Emi Sasaki - Ren Fujita', false)).toBeNull();
  });
  it('splits on the FIRST separator so a hyphenated member name still passes', () => {
    expect(engiEntryProblem('Mary-Jane Smith - Ren Fujita', false)).toBeNull();
  });
  it('refuses a name with no separator', () => {
    expect(engiEntryProblem('Emi Sasaki', false)).toBe(ENGI_PAIR_PROBLEM);
  });
  it('refuses an empty half, raw or trimmed', () => {
    for (const name of ['Alice -', '- Bob', '-', 'Alice - ', ' - Bob', ' - ']) {
      expect(engiEntryProblem(name, false)).toBe(ENGI_PAIR_PROBLEM);
    }
  });
  it('refuses a blank name', () => {
    for (const name of ['', '   ', null, undefined]) {
      expect(engiEntryProblem(name, false)).toBe(ENGI_PAIR_PROBLEM);
    }
  });
});

describe('engiEntryProblem, solo', () => {
  it('accepts one competitor', () => {
    expect(engiEntryProblem('Akira Tanaka', true)).toBeNull();
  });
  it('accepts a hyphenated name with no spaces around the hyphen', () => {
    expect(engiEntryProblem('Jean-Paul Martin', true)).toBeNull();
  });
  it('refuses a pair typed into a solo competition, raw or trimmed', () => {
    for (const name of ['A - B', 'Emi Sasaki - Ren Fujita', 'Alice -', '- Bob', 'Alice - ', ' - Bob', '-']) {
      expect(engiEntryProblem(name, true)).toBe(ENGI_SOLO_PROBLEM);
    }
  });
  it('refuses a blank name with its own sentence', () => {
    for (const name of ['', '   ', null, undefined]) {
      expect(engiEntryProblem(name, true)).toBe(ENGI_SOLO_BLANK);
    }
  });
});

describe('engiShape', () => {
  it('is null for a non-engi competition, whatever engiSolo says', () => {
    expect(engiShape(null)).toBeNull();
    expect(engiShape({})).toBeNull();
    expect(engiShape({ engi: false, engiSolo: true })).toBeNull();
  });
  it('is "pair" for engi without engiSolo, as every engi competition stored before the setting reads', () => {
    expect(engiShape({ engi: true })).toBe('pair');
    expect(engiShape({ engi: true, engiSolo: false })).toBe('pair');
  });
  it('is "solo" for engi with engiSolo', () => {
    expect(engiShape({ engi: true, engiSolo: true })).toBe('solo');
  });
});

describe('engiNameFieldLabel', () => {
  it('names the pair format only for a pair competition', () => {
    expect(engiNameFieldLabel('pair')).toBe('Pair (Name 1 - Name 2)');
    expect(engiNameFieldLabel('solo')).toBe('Name');
    expect(engiNameFieldLabel(null)).toBe('Name');
  });
});
