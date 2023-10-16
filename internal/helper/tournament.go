package helper

import (
	"fmt"
	"strings"
)

type Pool struct {
	PoolName string
	Players  []Player
	Matches  []Match

	// Excel coordinates
	sheetName string
	cell      string
}

type Player struct {
	Name string
	Dojo string

	PoolPosition int

	// Excel coordinates
	sheetName string
	cell      string
}
type MatchWinner struct {
	// Excel coordinates
	sheetName string
	cell      string
}

type Match struct {
	SideA *Player
	SideB *Player
}

func CreatePlayers(entries []string) []Player {
	players := make([]Player, 0)

	for _, entry := range entries {
		line := strings.Split(entry, ",")
		player := Player{
			Name: strings.TrimSpace(line[0]),
			Dojo: strings.TrimSpace(line[1]),
		}
		players = append(players, player)
	}

	return players
}

func CreatePools(players []Player, poolSize int) []Pool {
	totalPools := len(players) / poolSize
	// remainingEntries := len(players) % poolSize

	pools := make([]Pool, totalPools)

	for _, player := range players {
		poolN := discoverPool(pools, player, poolSize)
		if poolN < 0 {
			poolN = forcePoolSize(pools, player, poolSize)
			fmt.Printf("Added extra player to pool %d\n", poolN)
		}
		player.PoolPosition = len(pools[poolN].Players) + 1
		pools[poolN].Players = append(pools[poolN].Players, player)
	}

	for i := 0; i < len(pools); i++ {
		char := string('A' + i%26)
		if i > 25 {
			char = char + char
		}
		pools[i].PoolName = fmt.Sprintf("Pool %s", string(char))
	}

	return pools
}

func discoverPool(pools []Pool, player Player, poolSize int) int {

	for i, pool := range pools {

		// making sure there's space first
		if len(pool.Players) >= poolSize {
			continue
		}

		sameDojo := false
		for _, assignedPlayers := range pool.Players {
			// try make sure that there aren't other players of the same dojo
			if assignedPlayers.Dojo == player.Dojo ||
				assignedPlayers.Name == player.Name {
				// fmt.Println("Same dojo")
				sameDojo = true
			}
		}

		if !sameDojo {
			return i
		}

	}
	return -1
}

func forcePoolSize(pools []Pool, player Player, poolSize int) int {

	// TODO: still check if they are of the same Dojo
	for i, j := 0, len(pools)-1; i <= j; i, j = i+1, j-1 {
		if len(pools[i].Players) < poolSize+1 {
			return i
		}
		if i != j {
			if len(pools[j].Players) < poolSize+1 {
				return j
			}
		}
	}
	return 0
}

func alternateAdd(arr []int) []int {
	res := make([]int, 0, len(arr))
	for i, n := range arr {
		if i%2 == 0 {
			res = append([]int{n}, res...)
		} else {
			res = append(res, n)
		}
	}
	return res
}

func CreatePoolMatches(pools []Pool) {
	numPools := len(pools)

	for i := 0; i < numPools-1; i++ {
		// fmt.Println(pools[i].PoolName)
		for j := 0; j < len(pools[i].Players); j++ {

			p1 := pools[i].Players[0]
			p2 := pools[i].Players[1]
			if j%2 != 0 {
				p1, p2 = p2, p1
			}

			pools[i].Matches = append(pools[i].Matches, Match{
				SideA: &pools[i].Players[0],
				SideB: &pools[i].Players[1],
			})
			// fmt.Printf("%d vs %d\n", p1.PoolPosition, p2.PoolPosition)
			rotateTeams(pools[i].Players, 1)
		}
		rotateTeams(pools[i].Players, 1)
	}
}

func rotateTeams(teams []Player, shift int) {
	n := len(teams)
	shift %= n
	temp := make([]Player, shift)
	copy(temp, teams[:shift])
	copy(teams[:n-shift], teams[shift:])
	copy(teams[n-shift:], temp)
}

func CreatePoolRoundRobinMatches(pools []Pool) {

	for poolN, pool := range pools {
		// fmt.Println(pool.PoolName)
		size := len(pool.Players)

		matchIndex := 0
		var previousA *Player
		var previousB *Player
		for i := 0; i < size-1; i++ {

			for j := i + 1; j < size; j++ {
				matchIndex++
				sideA := &pools[poolN].Players[i]
				sideB := &pools[poolN].Players[j]

				if matchIndex%(size-1) == 0 {
					sideA, sideB = sideB, sideA
				}

				// restore if it was not time to change sides. This can happen with pools >4
				if previousA == sideB || previousB == sideA {
					sideA, sideB = sideB, sideA
				}
				pools[poolN].Matches = append(pools[poolN].Matches, Match{
					SideA: sideA,
					SideB: sideB,
				})

				// fmt.Printf("Match %d: %d vs %d\n", matchIndex, sideA.PoolPosition, sideB.PoolPosition)
				previousA = sideA
				previousB = sideB
			}
		}

	}

}
