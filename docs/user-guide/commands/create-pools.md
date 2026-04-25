# create-pools

Generates a **Pools & Playoffs** bracket: round-robin pools followed by a knockout elimination tree.

```
bracket-creator create-pools [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--file` | `-f` | — | CSV file with participants **(required)** |
| `--output` | `-o` | — | Output `.xlsx` path **(required)** |
| `--courts` | `-c` | `2` | Number of shiai-jo (courts) to distribute pools across |
| `--players` | `-p` | `3` | Minimum players per pool |
| `--max-players` | `-m` | — | Maximum players per pool |
| `--pool-winners` | `-w` | `2` | Players that qualify from each pool |
| `--round-robin` | `-r` | `false` | Force full round-robin in every pool |
| `--team-matches` | `-t` | `0` | Players per team (0 = individual tournament) |
| `--with-zekken-name` | `-z` | `false` | Use second CSV column as zekken display name |
| `--seeds` | — | — | CSV file with seed rankings |
| `--determined` | `-d` | `false` | Do not shuffle input order |
| `--single-tree` | — | `false` | Produce one tree sheet instead of one per court |
| `--mirror` | — | `true` | White on left, Red on right |
| `--title-prefix` | — | `""` | Prefix added to sheet titles |

## Examples

Minimal — two courts, random draw:

```bash
bracket-creator create-pools -f participants.csv -o tournament.xlsx
```

Three courts, pools of 4–5, top 2 qualify, full round-robin:

```bash
bracket-creator create-pools \
  -f participants.csv -o tournament.xlsx \
  -c 3 -p 4 -m 5 -w 2 -r
```

Team tournament (3 players per team, 2 courts):

```bash
bracket-creator create-pools \
  -f participants.csv -o tournament.xlsx \
  -t 3 -c 2
```

With zekken names and seeding:

```bash
bracket-creator create-pools \
  -f participants.csv -o tournament.xlsx \
  -z --seeds seeds.csv
```

## Seeding

Seeding distributes top competitors so they land in separate pools **and** on opposite sides of each court's elimination bracket.

Create a seeds CSV:

```
Rank,Name
1,Cersei Lannister
2,Daenerys Targaryen
3,Eddard Stark
```

Pass it with `--seeds seeds.csv`. Names must match the participant CSV exactly (case-sensitive). Unseeded participants are placed randomly around the seeds.

## Output sheets

The generated Excel file contains:

| Sheet | Contents |
|-------|----------|
| Data | Raw participant list |
| Time Estimator | Expected duration per phase |
| Pool Draw | Pool assignments grouped by court |
| Pool Matches | Individual match schedule |
| Elimination Matches | Knockout bracket match schedule |
| Names to Print | A3-ready name labels |
| Tree (one per court) | Visual bracket tree for display |
