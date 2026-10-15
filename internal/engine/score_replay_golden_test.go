package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// scoreReplayGolden is internal/engine/testdata/score_replay.json, the table
// the Go replay and its JS mirror (cases marked jsMirror) both answer.
type scoreReplayGolden struct {
	Cases []struct {
		Name     string `json:"name"`
		JSMirror bool   `json:"jsMirror"`
		Shape    struct {
			Kind     string `json:"kind"`
			Knockout bool   `json:"knockout"`
		} `json:"shape"`
		Match state.MatchResult    `json:"match"`
		Head  []state.ScoreLogLine `json:"head"`
		Lines []state.ScoreLogLine `json:"lines"`
		Want  struct {
			Match             map[string]any      `json:"match"`
			Outcomes          map[string][]string `json:"outcomes"`
			ClearedWithdrawal bool                `json:"clearedWithdrawal"`
		} `json:"want"`
	} `json:"cases"`
}

func goldenShape(kind string, knockout bool) (ReplayShape, error) {
	shape := ReplayShape{Knockout: knockout}
	switch kind {
	case "individual":
	case "engi":
		shape.Comp = &state.Competition{Engi: true}
	case "team":
		shape.Comp = &state.Competition{Kind: "team", TeamSize: 3}
	case "kachinuki":
		shape.Comp = &state.Competition{Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki}
	default:
		return shape, fmt.Errorf("unknown shape kind %q", kind)
	}
	return shape, nil
}

// emptyToNil reads an empty array as null, so a golden [] matches a nil
// slice the wire writes as null.
func emptyToNil(v any) any {
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			return nil
		}
		for i := range x {
			x[i] = emptyToNil(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = emptyToNil(x[k])
		}
	}
	return v
}

// subsetOf checks that every key want names is in got with the same value:
// objects are compared key by key (want may name fewer), arrays element by
// element at the same length.
func subsetOf(want, got any, path string) error {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want an object, got %v", path, got)
		}
		for k, v := range w {
			if err := subsetOf(v, g[k], path+"."+k); err != nil {
				return err
			}
		}
		return nil
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Errorf("%s: want %v, got %v", path, want, got)
		}
		for i := range w {
			if err := subsetOf(w[i], g[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	}
	if fmt.Sprint(want) != fmt.Sprint(got) {
		return fmt.Errorf("%s: want %v, got %v", path, want, got)
	}
	return nil
}

func TestReplayScoring_GoldenTable(t *testing.T) {
	raw, err := os.ReadFile("testdata/score_replay.json")
	require.NoError(t, err)
	var golden scoreReplayGolden
	require.NoError(t, json.Unmarshal(raw, &golden))
	require.NotEmpty(t, golden.Cases, "the table holds cases")
	mirrored := 0
	for _, c := range golden.Cases {
		if c.JSMirror {
			mirrored++
		}
		t.Run(c.Name, func(t *testing.T) {
			require.NotEmpty(t, c.Want.Outcomes, "every case pins outcomes")
			require.LessOrEqual(t, len(c.Lines), 6, "every order of more than 6 lines is too many")
			shape, err := goldenShape(c.Shape.Kind, c.Shape.Knockout)
			require.NoError(t, err)
			wantMatch := emptyToNil(c.Want.Match)
			for _, order := range permutations(len(c.Lines)) {
				lines := append([]state.ScoreLogLine(nil), c.Head...)
				for _, i := range order {
					lines = append(lines, c.Lines[i])
				}
				for _, l := range lines {
					require.NoError(t, l.Validate(), "order %v", order)
				}
				r := ReplayScoring(&c.Match, lines, shape)
				b, err := json.Marshal(r.Match)
				require.NoError(t, err)
				var got map[string]any
				require.NoError(t, json.Unmarshal(b, &got))
				require.NoError(t, subsetOf(wantMatch, emptyToNil(got), "match"), "order %v", order)
				gotOutcomes := map[string][]string{}
				for _, o := range r.Outcomes {
					gotOutcomes[o.ID] = append(gotOutcomes[o.ID], o.Outcome)
				}
				for id, want := range c.Want.Outcomes {
					g := slices.Clone(gotOutcomes[id])
					slices.Sort(g)
					w := slices.Clone(want)
					slices.Sort(w)
					require.Equal(t, w, g, "order %v, event %s", order, id)
				}
				require.Equal(t, c.Want.ClearedWithdrawal, r.ClearedWithdrawal, "order %v", order)
			}
		})
	}
	require.Positive(t, mirrored, "the JS mirror has cases to answer")
}
