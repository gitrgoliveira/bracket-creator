// list_phrase.jsx: how a list reads inside a line of prose. A leaf with no
// imports.
//
// A meta line is one phrase whose parts are separated by commas (operator
// decision R1, Hallmark gate 63: never a middle dot between them). A list
// INSIDE such a line, the shiaijo of a competition say, therefore takes
// "and" before its last item rather than more commas, so it cannot be read
// as further parts of the line: "11 May 2026, shiaijo A and B".

// andList: "A", "A and B", "A, B and C". Empty items are left out.
export function andList(items) {
  const xs = (items || []).filter((x) => x !== null && x !== undefined && x !== "");
  if (xs.length <= 1) return xs.join("");
  return `${xs.slice(0, -1).join(", ")} and ${xs[xs.length - 1]}`;
}
