# Input Format

Participants are provided as a plain CSV file — one participant per line, no header row.

## Basic format

```
Name, Dojo
Kevin Clark, Team Alpha
Luke Rodriguez, Team Beta
Michael Lewis, Team Gamma
```

The first column is the participant name. The second column is the dojo/team affiliation used to avoid placing dojo-mates in the same pool.

## Zekken display name

When using `--with-zekken-name` (`-z`), the second column provides the name printed on the zekken (name tag), and the third column provides the dojo/team affiliation. If the second column is empty the participant name is used instead.

```
Cersei Lannister, LANNISTER, Team Gamma
Daenerys Targaryen, TARGARYEN, Team Delta
Eddard Stark, STARK, Team Epsilon
```

## Team matches

For team tournaments (`--team-matches N`), each row still represents an individual fighter — the application groups them into teams of N. The dojo column is used to keep team-mates out of the same pool.

## Constraints

- Names must be **unique** — duplicate entries are rejected before any bracket is generated.
- Names in a [seeds file](commands/create-pools.md#seeding) must match the CSV exactly (case-sensitive).

## Seeds file

Seeding is a separate CSV with a header row:

```
Rank,Name
1,Cersei Lannister
2,Daenerys Targaryen
3,Eddard Stark
```

Pass it with `--seeds seeds.csv`. Only the listed participants are seeded; all others are placed randomly.
