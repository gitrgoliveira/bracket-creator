# Operating modes and access control

The app uses the word "mode" in three different contexts. This page covers two of them: **tournament mode** (who may act during the event) and **authentication mode** (how the admin password is stored and verified). The third, the digitization level you choose on the home screen, is described in [Three ways to run a tournament](../../index.md#three-ways-to-run-a-tournament).

## Tournament mode

Tournament mode controls who can score matches and manage competitions. You choose the mode once when you create a tournament; it cannot be changed afterwards.

### Officiated mode

Officiated mode is the default. Every action (scoring, check-in, starting and completing competitions) requires the admin password. All recorded results carry the label "admin" to show they were entered by an operator.

### Self-run mode

In self-run mode, scoring, check-in, and starting competitions are open to anyone without a password, so competitors or table helpers can run and score their own matches. Completing a competition is irreversible, so it stays behind the destructive-ops password (see [Destructive-ops password](#destructive-ops-password)). A public self-registration page also becomes available for competitors to sign themselves up.

Two kinds of action stay gated in self-run mode. Organiser setup (creating and editing competitions, tournament settings, seeds, scheduling, team lineups, match decisions such as kiken, and exports) still requires the admin password. Destructive actions (deleting a competition, discarding a draw, editing the roster, and completing a competition) require the destructive-ops password (see [Destructive-ops password](#destructive-ops-password)).

Results in self-run mode carry a provenance label. A score entered without a password is tagged "self-reported"; a score entered by an authenticated operator is tagged "admin". Officiated mode always produces "admin" results.

!!! note
    In file mode, self-run requires a destructive-ops password. Without one, destructive actions would be completely unprotected, so the app refuses to create or save a self-run tournament until a destructive-ops password is set (you can set it in the same step).

See the [Competitor self-run guide](../competitors/self-run.md) for the attendee-facing workflow.

## Admin authentication mode

Authentication mode controls how the admin password is stored and verified. There are two options: file mode (the default, suited for local or trusted-network events) and locked mode (recommended for internet-reachable deployments).

### File mode

In file mode the admin password is stored in plain text in the `tournament.md` data file. You set it during tournament creation, or you can edit the file directly.

If you forget the password, browse to `http://<host>/reset` from the same network and choose a new one. No old password is required. The reset path is intentionally unauthenticated, which is acceptable on a trusted private network but is a security risk on the public internet.

!!! note
    Switching from file mode to locked mode does not erase the on-disk password. The app ignores it while locked mode is active. Switching back re-enables the stored value.

### Locked mode

Locked mode is recommended for any deployment reachable from the internet. Instead of storing a password on disk, you supply a bcrypt hash through an environment variable and start the server with the `--lock-password` flag.

To set up locked mode:

1. Generate the hash with the `hash-password` command. The command reads the password from standard input with no prompt, and the terminal does not hide what you type, so pipe it in from a secrets manager or a here-doc rather than typing it interactively:

    ```
    printf '%s' "$MY_ADMIN_SECRET" | bracket-creator hash-password
    ```

2. Pass the hash and the flag when you start the server:

    ```
    TOURNAMENT_PASSWORD_HASH='$2a$10$...' bracket-creator mobile-app --lock-password -f ./tournament-data
    ```

In locked mode:

- The password stored in `tournament.md` is ignored for authentication.
- The `/reset` page shows an operator-disabled message, and the "Forgot password?" link is hidden.
- To rotate the credential, restart the server with a new hash.
- If `--lock-password` is set but `TOURNAMENT_PASSWORD_HASH` is empty or malformed, the server refuses to start (fail-closed).

See the [`hash-password` command reference](../commands/hash-password.md) for full details.

## Destructive-ops password

The destructive-ops password is a second, separately held credential that gates actions which are difficult to reverse. It lets table staff hold the main admin password for scoring, check-in, and match management without being able to accidentally or maliciously destroy data.

Actions gated by the destructive-ops password include:

- Deleting or invalidating a competition
- Discarding a generated draw
- Resetting manual overrides for a competition
- Adding, editing, or replacing participants
- Importing competitions
- Completing a competition (completing is irreversible)

The following are NOT gated by the destructive-ops password: scoring, match decisions, check-in, starting a competition, lineup management, or the `/reset` path.

The admin UI prompts for the destructive-ops password each time a gated action is requested. There is no cached session between prompts.

### In file mode

Set the destructive-ops password from **Admin > Edit details > Destructive-ops password**. The field is write-only and is never displayed. Changing it after it has been set requires supplying the current value first. While the field is unset, gated actions fall back to requiring the main admin password only.

### In locked mode

Supply the destructive-ops password as a bcrypt hash in the `TOURNAMENT_ADMIN_PASSWORD_HASH` environment variable. It cannot be changed through the UI.

```
printf '%s' "$MY_DESTRUCTIVE_OPS_SECRET" | bracket-creator hash-password

TOURNAMENT_PASSWORD_HASH='$2a$10$...main...' \
  TOURNAMENT_ADMIN_PASSWORD_HASH='$2a$10$...destructive...' \
  bracket-creator mobile-app --lock-password -f ./tournament-data
```

In locked mode the destructive-ops gate is always active. If `TOURNAMENT_ADMIN_PASSWORD_HASH` is unset or malformed, the gated actions are refused regardless of the admin password supplied. A malformed hash does not prevent the server from starting, but no destructive action can be completed until a valid hash is provided.

## Security notes

The admin and destructive-ops passwords provide privilege separation for shared-credential operation at an event. They are not a network-security boundary on their own.

Over plain HTTP, both passwords travel in clear text on every request. Anyone with filesystem access to `tournament.md` can read both passwords in file mode.

For a real security boundary:

- Run the server behind TLS. See [Hosting](../install/hosting.md) for guidance.
- Use locked mode for any deployment reachable from the internet.
- Be aware that in file mode the password reset is unauthenticated: anyone who can reach the server can set a new admin password without knowing the current one, which locks out the operator or takes over the tournament. Locked mode disables reset entirely, so any internet-exposed deployment should use locked mode.
