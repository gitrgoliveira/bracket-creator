# serve

Starts the web UI so you can generate brackets from a browser without using the command line.

```
bracket-creator serve [flags]
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `8080` | Port to listen on |
| `--bind` | `-b` | `localhost` | Address to bind to |

## Usage

```bash
# Default — available at http://localhost:8080
bracket-creator serve

# Different port
bracket-creator serve -p 8081

# Accessible from other machines on the network
bracket-creator serve -b 0.0.0.0
```

See the [Web UI guide](../web-ui.md) for a walkthrough of the interface.
