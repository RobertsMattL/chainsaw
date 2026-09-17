# ⛓ chainsaw

A terminal log viewer with saved configurations, live filtering, and syntax highlighting. Tail local files, run log commands, or stream from remote hosts over SSH — all with an interactive TUI.

## Install

```sh
curl -sSfL https://raw.githubusercontent.com/RobertsMattL/chainsaw/main/install.sh | sh
```

Installs to `/usr/local/bin/chainsaw`. To install elsewhere:

```sh
CHAINSAW_INSTALL=~/bin curl -sSfL https://raw.githubusercontent.com/RobertsMattL/chainsaw/main/install.sh | sh
```

**Supported platforms:** macOS (Intel + Apple Silicon), Linux (amd64, arm64, armv7 for Raspberry Pi)

## Quick start

```sh
chainsaw config add        # create a named log configuration
chainsaw watch             # pick a config interactively
chainsaw watch <name>      # tail a specific config
chainsaw config list       # list saved configs
```

## Configuration

Configs are stored at `~/Library/Application Support/chainsaw/configs.yaml` (macOS) or `~/.config/chainsaw/configs.yaml` (Linux).

### Source types

**File** — tail a local file or glob:

```yaml
configs:
  - name: app
    sources:
      - type: file
        path: /var/log/app/output.log
```

**Command** — stream output of a local shell command:

```yaml
configs:
  - name: docker-app
    sources:
      - type: command
        command: docker logs -f my-container
```

**SSH** — run a command on a remote host and stream the output:

```yaml
configs:
  - name: beam
    sources:
      - type: ssh
        host: tank@100.68.160.75
        command: "tail -f ~/.somewear/beam/log/output.log | grep --line-buffered -oE '(Rx|Tx).*'"
        label: beam
```

SSH uses your existing keys (`~/.ssh`). `BatchMode=yes` is set so it won't prompt for passwords — add your key to the remote host with `ssh-copy-id` first.

### Multiple sources

Watch several streams side-by-side in one session:

```yaml
configs:
  - name: fleet
    sources:
      - type: ssh
        host: tank@100.68.160.75
        command: "tail -f ~/.somewear/beam/log/output.log"
        label: tank
      - type: file
        path: /var/log/local-service.log
        label: local
```

### Highlighting

```yaml
configs:
  - name: app
    sources:
      - type: file
        path: /var/log/app.log
    json_mode: true
    highlights:
      - pattern: '\b(ERROR|ERR)\b'
        style: red+bold
        mode: line
      - pattern: '\b(WARN)\b'
        style: yellow
        mode: line
      - pattern: 'user_id=\S+'
        style: cyan
        mode: match
```

Default highlights (ERROR/WARN/INFO/DEBUG/TRACE) are applied automatically unless you define your own or pass `--no-defaults`.

## TUI controls

| Key | Action |
|-----|--------|
| `/` | Open filter bar |
| `Enter` / `Esc` | Close filter (Esc clears it) |
| `↑ ↓` / `PgUp PgDn` | Scroll |
| `g` / `G` | Jump to top / bottom |
| `q` / `Ctrl+C` | Quit |

## Watch flags

```
--json          force JSON pretty-print mode
--include RE    only show lines matching regex
--exclude RE    hide lines matching regex
--no-color      disable highlighting
--no-defaults   skip default log-level highlights
```

## Build from source

```sh
git clone https://github.com/RobertsMattL/chainsaw.git
cd chainsaw
go build -o chainsaw .
```

Requires Go 1.22+.
