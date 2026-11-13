// Matching the app's visible text: the one regexp escape every fixture and
// journey uses to build a pattern from a label, a name or a sentence.
export const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
