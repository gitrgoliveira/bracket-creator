// bronze_feeders.jsx: the ONE owner of which semi-final feeds which side of the
// 3rd-place match. The loser of the first semi-final (index 0 of the round
// before the final) takes the bronze's side A, the loser of the second its
// side B, as the server seats them (Go propagateBracketWinner). A leaf with no
// imports, so the court console's pending-row test (admin_helpers.jsx) and the
// bracket's empty-side labels (bracket.jsx) ask the same rule without
// importing each other.
//
// Returns { a, b }: each side's feeding semi-final match, or null when the
// rounds hold no such match (fewer than two rounds, or a missing semi).
export function bronzeFeederSemis(rounds) {
  const rs = Array.isArray(rounds) ? rounds : [];
  const semis = rs.length >= 2 ? (rs[rs.length - 2] || []) : [];
  return { a: semis[0] || null, b: semis[1] || null };
}
