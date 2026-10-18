# Operating modes and access control

The app uses the word "mode" in three different contexts. This page covers two of them: **tournament mode** (who may act during the event) and **authentication mode** (how the admin password is stored and verified). The third, the digitisation level you choose on the home screen, is described in [Three ways to run a tournament](../../index.md#three-ways-to-run-a-tournament).

## Tournament mode

Tournament mode controls who can score matches and manage competitions. You choose the mode once when you create a tournament; it cannot be changed afterwards.

### Officiated mode

Officiated mode is the default. Every action (scoring, check-in, starting, and completing competitions) requires the admin password. Once you are signed in, one action asks for the password again: **Delete competition** (in the competition's Settings) opens a dialog that names the competition and asks you to type the admin password. A wrong password leaves the dialog open with "Wrong password. The competition was not deleted." and you stay signed in. The server checks the password typed in the dialog, so a request that does not carry it is refused too. Every other action, discarding a draw and marking a competition invalid included, needs no second password. All recorded results carry the label "admin" to show they were entered by an operator.

### Self-run mode

In self-run mode, scoring and starting a competition are open to anyone without a password, so competitors or table helpers can run and score their own matches. Completing a competition needs no password either: in self-run mode the server accepts it from anyone. The **Complete competition** button is on the organiser's competition page, which you reach by signing in. A public self-registration page also becomes available for competitors to sign themselves up, for individual competitions only and only while the competition is still in setup, before the draw is generated.

Organiser actions stay behind the admin password in self-run mode: creating and editing competitions, tournament settings, the tournament logo and sponsor images, seeds, scheduling, the **Lineups** page, match decisions such as kiken, result corrections (reopening a match, setting a winner by hand, overriding a pool rank), announcements, league tie-breaker decisions (adding or removing tie-breaker matches, and accepting shared ranks), exports, fighting-spirit awards, and every action that destroys data (deleting or invalidating a competition, discarding a draw, resetting manual overrides, and importing competitions).

The roster is open to anyone in self-run mode: anyone can add and edit a competition's participants, without the password, before the competition starts.

Self-run mode has no check-in. The **Check-in tracking** setting is not offered, the **Registration desk** is not offered, the server refuses every check-in request, and the draw includes every participant on the roster. A competition keeps the check-in setting it was saved with, but a self-run tournament ignores it.

Competitors can name the fighters in a team match from its score sheet, without the password. A team member picked for a bout, or a typed name, is saved with that match, and a typed name fills in a team member who has no name yet or adds a new one. Two changes stay with the organiser: the lineup of a match that has finished, and the name of a team member who already has one.

Competitors also run the representative bout (daihyosen) of a tied knockout team match from its score sheet while the match is in progress: they add it, score it like any other bout, and remove one added by mistake while it is still unscored. Once the match has finished, adding or removing its representative bout stays with you. A kachinuki match has no representative bout: a tied pairing that must produce a result is settled by encho on that same bout (refer to [Scoring a kachinuki encounter](team-tournaments.md#scoring-a-kachinuki-encounter)). A judges' decision (hantei) stays with the organiser, on the representative bout as on every other bout, so the public score sheet does not offer one. Once you record one, the public score sheet shows that bout as decided, and a competitor can finish the match on it but cannot change or clear it.

A competitor's taps on the public score sheet are sent as they are made, in batches when several are waiting, exactly as an organiser's are, and they are kept on the device and sent in order if the connection drops. There is no save step to forget. Once a match has finished, the sheet takes no further change from a competitor: a tap on a finished match is refused with "This match result has already been reported. Contact the tournament organizer to correct it.", and changing a finished result stays with the organiser's **Correct**.

Results in self-run mode carry a provenance label. A score entered without a password is tagged "self-reported"; a score entered by an authenticated operator is tagged "admin". Officiated mode always produces "admin" results.

Refer to the [Competitor self-run guide](../competitors/self-run.md) for the attendee-facing workflow.

## Admin authentication mode

Authentication mode controls how the admin password is stored and verified. There are two options: file mode (the default, suited for local or trusted-network events) and locked mode (recommended for internet-reachable deployments).

### File mode

In file mode, the admin password is stored in plain text in the `tournament.md` data file. You set it during tournament creation, or you can edit the file directly.

If you forget the password, browse to `http://localhost:<port>/reset` in a browser on the machine running the server and choose a new one. No old password is required. In file mode the password can be reset only from that machine: a request from any other machine, or one addressed by a name other than `localhost`, `127.0.0.1` or `[::1]`, is refused with "Reset the password on the machine running the server." When the server runs in a Docker container started with `-p`, a browser on the host reaches it through the container network, not loopback, so the reset is refused there too; edit `tournament.md` in the mounted data folder instead.

!!! note
    Switching from file mode to locked mode does not erase the on-disk password. The app ignores it while locked mode is active. Switching back re-enables the stored value.

### Locked mode

Locked mode is recommended for any deployment reachable from the internet. Instead of storing a password on disk, you supply a bcrypt hash through an environment variable and start the server with the `--lock-password` flag.

To set up locked mode:

1. Generate the hash with the `hash-password` command. The command reads the password from standard input with no prompt, and the terminal does not hide what you type. Pipe it in from a secrets manager or a here-doc rather than typing it interactively:

    ```bash
    printf '%s' "$MY_ADMIN_SECRET" | bracket-creator hash-password
    ```

2. Pass the hash and the flag when you start the server:

    ```bash
    TOURNAMENT_PASSWORD_HASH='$2a$10$...' bracket-creator mobile-app --lock-password -f ./tournament-data
    ```

In locked mode:

- The password stored in `tournament.md` is ignored for authentication.
- The `/reset` page shows an operator-disabled message, and the "Forgot password?" link is hidden.
- To rotate the credential, restart the server with a new hash.
- If `--lock-password` is set but `TOURNAMENT_PASSWORD_HASH` is empty or malformed, the server refuses to start (fail-closed).

Refer to the [`hash-password` command reference](../commands/hash-password.md) for full details.

## Security notes

The admin password is not a network-security boundary on its own.

Over plain HTTP, the password travels in clear text on every request. Anyone with filesystem access to `tournament.md` can read it in file mode.

For a real security boundary:

- Run the server behind TLS. Refer to [Hosting](../install/hosting.md) for guidance.
- Use locked mode for any deployment reachable from the internet.
- In file mode, the password reset needs no current password but is limited to the machine running the server, so someone who can open a browser on that machine can set a new admin password. Behind a reverse proxy on the same machine, requests arrive from the proxy's own address, so block `/reset` at the proxy. Locked mode turns reset off, so any internet-exposed deployment should use locked mode.

### Upgrading from an earlier release

The admin password is the only organiser password. Anyone who signs in with it can do every organiser action, including discarding a draw, and deleting a competition once they type the password again. If some staff should not be able to do this, give the tournament a new admin password and share it only with the people who should have it.

A `tournament.md` that carries an `admin_password` field loads normally; the field is ignored. It may hold another password in plain text. The app drops the field the next time it writes `tournament.md`, for example when you change the tournament settings; until then, remove the line by hand before you share a copy of the data folder. The `TOURNAMENT_ADMIN_PASSWORD_HASH` environment variable is ignored: when it is set, the server logs a line at startup saying so, and you can remove it from your start-up scripts.
