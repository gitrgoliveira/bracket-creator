# Input Format

Participants are provided as a plain CSV file (one participant per line, no header row).

## Basic format

```
Name, Dojo
Kevin Clark, Team Alpha
Luke Rodriguez, Team Beta
Michael Lewis, Team Gamma
```

The first column is the participant name. The second column is the dojo or team affiliation. **Both are required**: a row with no dojo is rejected, because a competitor is identified by name and dojo together, and two competitors who share a name can only be told apart by it. The dojo is what the draw uses to keep dojo-mates apart, in the pools and in the first round of a knockout.

## Zekken display name

When using `--with-zekken-name` (`-z`), the second column provides the name printed on the zekken (name tag), and the third column provides the dojo/team affiliation. If the second column is empty the participant name is used instead.

```
Cersei Lannister, LANNISTER, Team Gamma
Daenerys Targaryen, TARGARYEN, Team Delta
Eddard Stark, STARK, Team Epsilon
```

## Team matches

For team tournaments (`--team-matches N`), each row still represents an individual fighter. The application groups them into teams of N. The dojo column is used to keep team-mates out of the same pool.

## Constraints

- A competitor is identified by **name and dojo together**. Two people who share a name are accepted as long as their dojos differ, which is common with widespread surnames; the same name at the same dojo is one person entered twice and is rejected before any bracket is generated.
- The dojo must not be blank. A roster stored before this rule existed, or edited by hand, still loads so it can be repaired, but until the blank dojo is fixed the app refuses every change to that roster (check-ins included) and refuses to generate a draw from it; each refusal names the participant to fix. Importing a saved tournament is refused the same way: correct the dojo in the archive's participant file and import again.
- **Team names are the exception**: two teams may not share a name even at different dojos, because a team's name is what identifies it in results.
- Names in a [seeds file](../commands/create-pools.md#seeding) must match the CSV exactly (case-sensitive).

## Seeds file

Seeding is a separate CSV with a header row:

```
Rank,Name
1,Cersei Lannister
2,Daenerys Targaryen
3,Eddard Stark
```

Pass it with `--seeds seeds.csv`. Only the listed participants are seeded; all others are placed randomly.
