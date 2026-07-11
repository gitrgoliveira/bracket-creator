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
| `--courts` | `-c` | `2` | Number of shiai-jo (courts) to split the tree across |
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
