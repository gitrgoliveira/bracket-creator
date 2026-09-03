# serve

Starts the web UI so you can generate brackets from a browser without using the command line.

```
bracket-creator serve [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `8080` (or `$PORT`) | Port to listen on |
| `--bind` | `-b` | `localhost` (or `$BIND_ADDRESS`) | Address to bind to |

## Usage

Start with the defaults. The UI is at `http://localhost:8080`:

```bash
bracket-creator serve
```

Use a different port:

```bash
bracket-creator serve -p 8081
```

Make it reachable from other machines on the network:

```bash
bracket-creator serve -b 0.0.0.0
```

Refer to the [Legacy Web UI guide](../organisers/web-ui.md) for a walkthrough of the interface.
