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
| `--courts` | `-c` | `2` | Number of shiai-jo (courts) to split the tree across. Must be 1, 2, 4, 8 or 16, see [Shiai-jo count](#shiai-jo-count) |
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

`--courts` takes **1, 2, 4, 8 or 16**. The tree is split into one block per shiai-jo and those blocks merge in pairs, so the count has to halve cleanly all the way down. Any other value stops the command with an error naming the counts to use instead, and the error always offers 1.

Being even is not enough on its own. With `-c 6` the six blocks pair off into three, and three cannot pair off again, so one of them would reach the final having fought a round fewer than the other two. `-c 6` and `-c 10` are therefore refused, just as `-c 3`, `-c 5` and `-c 7` are.

A single shiai-jo is always allowed: `-c 1` prints the whole bracket as one shiai-jo's pages. A playoffs bracket has no pool finishers to cross between shiai-jo, so it is seeded first and then cut into shiai-jo blocks; the partner-shiai-jo crossing described for [create-pools](create-pools.md#shiai-jo-count) does not apply here. 16 is the highest, and it is also the most shiai-jo a tournament can have, which puts 32 out of reach.

This is a per-tournament-file rule, not a rule about your venue. A hall with three shiai-jo generates one file for two shiai-jo and another for one, and runs both at the same time. See [create-pools](create-pools.md#shiai-jo-count) for the same rule on the pools command, and [How many shiai-jo a competition can use](../organisers/knockout-draw.md#how-many-shiai-jo-a-competition-can-use) for the full explanation.

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
