package mobileapp

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// A copy whose stamp moves back (an earlier change of its own arrives late)
// carries, at the earlier stamp, only what was in force then: a position
// carried from a source made after that stamp is empty there, never the
// value carried at the later stamp (stage 6 review 8, M1).
func TestLineupCarry_ACopyMovedBackCarriesNothingMadeAfterIt(t *testing.T) {
	for _, late := range []bool{false, true} {
		e := newCarryEnv(t, false)
		if !late {
			e.save("P1-1", 1, "Mori", 10)
		}
		e.save("P1-0", 3, "Endo", 50)
		e.save("P1-1", 2, "Doi", 100)
		if late {
			e.save("P1-1", 1, "Mori", 10)
		}
		got := e.dump()
		require.Equalf(t, "start{1=Sato 2=Ueda} P1-0{1=Sato 2=Ueda 3=Endo} P1-1{1=Mori 2=Doi}", got, "late=%v", late)
	}
}

// The same when a kachinuki start's pin becomes a copy at a stamp before the
// start: what the pin carried from a lineup made after that stamp goes.
func TestLineupCarry_APinMadeACopyEarlierCarriesNothingMadeAfterIt(t *testing.T) {
	for _, late := range []bool{false, true} {
		e := newCarryEnv(t, true)
		if !late {
			e.save("P1-1", 2, "Doi", -466)
		}
		e.save("P1-0", 3, "Endo", -167)
		e.start("P1-1", "Washi", -143)
		if late {
			e.save("P1-1", 2, "Doi", -466)
		}
		got := e.dump()
		require.Equalf(t, "start{1=Sato 2=Ueda} P1-0{1=Sato 2=Ueda 3=Endo} P1-1{1=Sato 2=Doi}", got, "late=%v", late)
	}
}

// randomCarryOp is one step of a random lineup scenario.
type randomCarryOp struct {
	kind  string // "save" or "start"
	match string
	pos   int
	name  string
	at    int64
}

// playRandomCarry plays ops in order on a fresh competition and answers every
// lineup as stored, and bout 1 of each running match.
func playRandomCarry(t *testing.T, kachinuki bool, ops []randomCarryOp, order []int) string {
	t.Helper()
	e := newCarryEnv(t, kachinuki)
	opponent := map[string]string{"P1-0": "Kuma", "P1-1": "Washi", "P1-2": "Inu"}
	for _, i := range order {
		o := ops[i]
		if o.kind == "start" {
			e.start(o.match, opponent[o.match], o.at)
			continue
		}
		e.save(o.match, o.pos, o.name, o.at)
	}
	s := e.dump()
	for _, m := range []string{"P1-0", "P1-1", "P1-2"} {
		if e.stored(m).Status == "running" {
			s += " bout1(" + m + ")=" + e.boutOne(m)
		}
	}
	return s
}

// Random partial saves of the starting lineup and of three matches' own
// lineups (and, on kachinuki, a start), at random stamps: every arrival
// order tried gives the lineups the changes give in the order they were made.
// Seeds 32 and 187 failed before stage 6 review 8 (M1).
func TestLineupCarry_RandomSavesAreTheSameInEveryArrivalOrder(t *testing.T) {
	names := map[int][]string{1: {"Sato", "Mori", "Aoki"}, 2: {"Ueda", "Doi"}, 3: {"Endo", "Kato"}}
	matches := []string{"", "P1-0", "P1-1", "P1-2"}
	seeds := []int64{32, 187}
	for s := int64(1); s <= 24; s++ {
		seeds = append(seeds, s)
	}
	for _, seed := range seeds {
		rng := rand.New(rand.NewSource(seed))
		kachinuki := seed%2 == 0
		used := map[int64]bool{}
		stamp := func() int64 {
			for {
				s := int64(rng.Intn(1000) - 500)
				if !used[s] && s != 0 {
					used[s] = true
					return s
				}
			}
		}
		var ops []randomCarryOp
		for range 4 + rng.Intn(3) {
			p := 1 + rng.Intn(3)
			ops = append(ops, randomCarryOp{"save", matches[rng.Intn(4)], p, names[p][rng.Intn(len(names[p]))], stamp()})
		}
		if kachinuki {
			ops = append(ops, randomCarryOp{kind: "start", match: matches[1+rng.Intn(2)], at: stamp()})
		}
		ref := make([]int, len(ops))
		for i := range ref {
			ref[i] = i
		}
		slices.SortFunc(ref, func(a, b int) int { return int(ops[a].at - ops[b].at) })
		want := playRandomCarry(t, kachinuki, ops, ref)
		rev := slices.Clone(ref)
		slices.Reverse(rev)
		orders := [][]int{rev}
		for range 2 {
			o := slices.Clone(ref)
			rng.Shuffle(len(o), func(i, j int) { o[i], o[j] = o[j], o[i] })
			orders = append(orders, o)
		}
		for _, order := range orders {
			got := playRandomCarry(t, kachinuki, ops, order)
			require.Equalf(t, want, got, "seed %d (kachinuki %v), ops %+v, arrival order %v", seed, kachinuki, ops, order)
		}
	}
}
