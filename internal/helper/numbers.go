package helper

import "fmt"

// AssignPlayerNumbers sets Number on each player to prefix+counter, where counter
// starts at start and increments by one. Returns the next counter value so callers
// can chain across multiple slices (e.g. pools).
func AssignPlayerNumbers(players []Player, prefix string, start int) int {
	for i := range players {
		players[i].Number = fmt.Sprintf("%s%d", prefix, start+i)
	}
	return start + len(players)
}
