# hash-password

Produces a bcrypt hash for use with the [`mobile-app`](mobile-app.md) command's locked-password mode. Operators set the resulting hash in the `TOURNAMENT_PASSWORD_HASH` environment variable so the running server can compare incoming `X-Tournament-Password` headers against it without storing the plaintext.

```
bracket-creator hash-password [plaintext]
```

## Input

The command reads the plaintext from one of two sources, in this order:

1. **Positional argument** — `bracket-creator hash-password mysecret`. Convenient for ad-hoc use, but the password is recorded in shell history. Suitable for development.
2. **Standard input** (when no argument is supplied) — type the password and press Return. The terminal echoes it (no `-s`-style hiding); pair with a here-doc or piped secret manager for production rotation. Recommended path because it avoids shell-history leakage.

Bcrypt has a hard 72-byte limit on the input. Passwords longer than that are rejected up-front rather than silently truncated.

## Output

Single line on stdout: the bcrypt hash (e.g. `$2a$10$tq9jkGYsf1ttx0ZM.UUrxezVBcO4aZaS.dVRY73xC5lwEvTJLcMc6`). Cost is `bcrypt.DefaultCost` (10) — fine for admin-facing endpoints (~50–100 ms per verify) and not worth tuning until a real load problem appears.

## Examples

```bash
# Quick generation via argument (leaves password in shell history)
bracket-creator hash-password mysecret

# Stdin path — type the password, press Return
bracket-creator hash-password
# (prompt is silent; type and Enter)

# Capture into a shell variable for the mobile-app server
HASH=$(printf '%s' "$MY_SECRET" | bracket-creator hash-password)
TOURNAMENT_PASSWORD_HASH="$HASH" \
  bracket-creator mobile-app --lock-password -f ./tournament-data
```

See the [mobile-app command reference](mobile-app.md#locked-mode---lock-password) for how the hash is consumed at startup.
