# Input format

Participants are provided as a plain CSV file (one participant per line, no header row).

## Basic format

```csv
Name, Dojo
Kevin Clark, Team Alpha
Luke Rodriguez, Team Beta
Michael Lewis, Team Gamma
```

The first column is the participant name. The second column is the dojo or team affiliation. **Both are needed**: a competitor is identified by name and dojo together, and two competitors who share a name can only be told apart by it. The tournament app rejects a row with no dojo. The command line does not: a row with only a name is read with the dojo `NA`, so every such row counts as one shared dojo for the draw. Further columns are accepted and kept as metadata, such as a dan grade; they do not affect the draw.

## Zekken display name

When using `--with-zekken-name` (`-z`), the second column provides the name printed on the zekken (name tag), and the third column provides the dojo/team affiliation. If the second column is empty the participant name is used instead.

```csv
Cersei Lannister, LANNISTER, Team Gamma
Daenerys Targaryen, TARGARYEN, Team Delta
Eddard Stark, STARK, Team Epsilon
```

## Team matches

For team tournaments (`--team-matches N`), each row is one team: the name column holds the team's name and the dojo column its affiliation. `N` is the number of fighters per team, and sets how many bout rows print under each team encounter in the spreadsheet, for the fighters' names to be filled in by hand. The dojo column keeps teams from the same dojo apart across pools, the same rule individual mode uses.

## Constraints

- A competitor is identified by **name and dojo together**. Two people who share a name are accepted as long as their dojos differ, which is common with widespread surnames; the same name at the same dojo is one person entered twice and is rejected before any bracket is generated.
- The dojo must not be blank. A roster stored before this rule existed, or edited by hand, still loads so it can be repaired. Until the blank dojo is fixed, the app refuses every change to that roster (check-ins included) and refuses to generate a draw from it, and each refusal names the participant to fix. Importing a saved tournament is refused the same way: correct the dojo in the archive's participant file and import again.
- **Team names are the exception**: two teams may not share a name even at different dojos, because a team's name is what identifies it in results.
- Names in a [seeds file](../commands/create-pools.md#seeding) must name a participant in the CSV. Matching ignores case and accents, because both lists are normalised the same way when read.

## Seeds file

Seeding is a separate CSV with a header row:

```csv
Rank,Name
1,Cersei Lannister
2,Daenerys Targaryen
3,Eddard Stark
```

Pass it with `--seeds seeds.csv`. Only the listed participants are seeded; all others are placed randomly. A rank given to two names, or a name that is not in the participant list, stops the run with an error naming the offender.
