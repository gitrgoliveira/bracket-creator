# create-playoffs

Generates a **Playoffs Only** bracket: a direct single-elimination tree with no pool phase.

```
bracket-creator create-playoffs [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--file` | `-f` | (none) | CSV file with participants **(required)** |
| `--output` | `-o` | (none) | Output `.xlsx` path **(required)** |
| `--courts` | `-c` | `2` | Number of shiai-jo (courts) to split the tree across. Must be 1 or an even number, see [Shiai-jo count](#shiai-jo-count) |
| `--team-matches` | `-t` | `0` | Players per team (0 = individual tournament) |
| `--with-zekken-name` | `-z` | `false` | Use second CSV column as zekken display name |
| `--seeds` | (none) | (none) | CSV file with seed rankings |
| `--determined` | `-d` | `false` | Do not shuffle input order |
| `--single-tree` | (none) | `false` | Produce one tree sheet instead of one per court |
| `--number-prefix` | `-n` | `""` | Assign consecutive numbers with this letter prefix (for example, `K` produces K1, K2, …) |
| `--title-prefix` | (none) | `""` | Prefix added to sheet titles |

## Examples

Simple two-court bracket:

```bash
bracket-creator create-playoffs -f participants.csv -o tournament.xlsx
```

Single court, seeded:

```bash
bracket-creator create-playoffs \
  -f participants.csv -o tournament.xlsx \
  -c 1 --seeds seeds.csv
```

Team tournament across two courts with zekken names:

```bash
bracket-creator create-playoffs \
  -f participants.csv -o tournament.xlsx \
  -t 3 -c 2 -z
```

## Shiai-jo count

`--courts` takes **1 or an even number**. The tree is split into one block per shiai-jo and those blocks are paired up, so an odd number above 1 leaves one shiai-jo without a partner and the command stops with an error naming the counts to use instead.

A single shiai-jo is always allowed: `-c 1` splits its block into two halves that act as partner shiai-jo, producing the same bracket shape as a multi-shiai-jo run. The upper bound is 26, because shiai-jo are labelled A to Z.

This is a per-tournament-file rule, not a rule about your venue. A hall with five shiai-jo can generate one file for four shiai-jo and another for one. See [create-pools](create-pools.md#shiai-jo-count) for the same rule on the pools command.

## Seeding

Works the same as `create-pools`; top seeds are placed on opposite sides of the bracket so they can only meet in the final. See the [input format](../organisers/input-format.md#seeds-file) for the seeds CSV layout.

## Output sheets

| Sheet | Contents |
|-------|----------|
| Data | Raw participant list |
| Time Estimator | Expected duration per phase |
| Elimination Matches | Match schedule |
| Names to Print | A3-ready name labels |
| Tree (one per court) | Visual bracket tree |
