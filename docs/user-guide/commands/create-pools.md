# create-pools

Generates a **Pools & Playoffs** bracket: round-robin pools followed by a knockout elimination tree.

```
bracket-creator create-pools [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--file` | `-f` | (none) | CSV file with participants **(required)** |
| `--output` | `-o` | (none) | Output `.xlsx` path **(required)** |
| `--courts` | `-c` | `2` | Number of shiai-jo (courts) to distribute pools across. Must be 1 or an even number, see [Shiai-jo count](#shiai-jo-count) |
| `--players` | `-p` | `3` | Minimum players per pool |
| `--max-players` | `-m` | (none) | Maximum players per pool |
| `--pool-winners` | `-w` | `2` | Players that qualify from each pool |
| `--round-robin` | `-r` | `false` | Force full round-robin in every pool |
| `--team-matches` | `-t` | `0` | Players per team (0 = individual tournament) |
| `--with-zekken-name` | `-z` | `false` | Use second CSV column as zekken display name |
| `--seeds` | (none) | (none) | CSV file with seed rankings |
| `--determined` | `-d` | `false` | Do not shuffle input order |
| `--single-tree` | (none) | `false` | Produce one tree sheet instead of one per court |
| `--number-prefix` | `-n` | `""` | Assign consecutive numbers with this letter prefix (for example, `K` produces K1, K2, …) |
| `--title-prefix` | (none) | `""` | Prefix added to sheet titles |

## Examples

Minimal: two courts, random draw:

```bash
bracket-creator create-pools -f participants.csv -o tournament.xlsx
```

Four courts, pools of 4–5, top 2 qualify, full round-robin:

```bash
bracket-creator create-pools \
  -f participants.csv -o tournament.xlsx \
  -c 4 -p 4 -m 5 -w 2 -r
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

## Shiai-jo count

`--courts` takes **1 or an even number**. The elimination tree gives each shiai-jo its own block of the bracket and pairs those blocks up, so each pool's runner-up crosses into the partner shiai-jo's block. An odd number above 1 leaves one shiai-jo without a partner, and the command stops with an error naming the counts to use instead.

A single shiai-jo is always allowed: `-c 1` splits its block into two halves that act as partner shiai-jo, producing the same bracket shape as a multi-shiai-jo run. The upper bound is 26, because shiai-jo are labelled A to Z.

This is a per-tournament-file rule, not a rule about your venue. A hall with five shiai-jo can generate one file for four shiai-jo and another for one.

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
