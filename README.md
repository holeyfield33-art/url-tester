# url-tester

Standalone terminal utility for running commands against a target URL inside an isolated Docker container.

Extracted and simplified from [PentAGI](https://github.com/vxcontrol/pentagi) `cmd/ftester`, focused on the **terminal** function so teams can probe targets without the full PentAGI stack (no Postgres, no LLM providers, no flow IDs).

## Features

- Accepts a **target URL** (`-target`) and injects it as `$TARGET_URL` inside the container
- Runs arbitrary shell commands in a disposable Docker environment (default: Kali-based image)
- Same CLI style as original ftester: `terminal -input "<command>"`
- Optional detach mode, timeout control, keep-container for follow-up inspection
- Minimal dependencies — only the Docker Go client

## Requirements

- Go 1.22+
- Docker daemon reachable via the default socket / `DOCKER_HOST`

## Install / Build

```bash
git clone https://github.com/holeyfield33-art/url-tester.git
cd url-tester
go mod tidy   # downloads deps and creates go.sum
go build -o url-tester .
```

Or run directly:

```bash
go run . -target https://example.com terminal -input 'curl -sI "$TARGET_URL"'
```

## Usage

```text
url-tester -target <URL> terminal -input "<command>" [options]
```

### Core flags

| Flag | Description |
|------|-------------|
| `-target` | Target URL (required for most uses). Becomes `$TARGET_URL` inside the container. |
| `-input` / `-command` | Shell command to execute (required). |

### Additional options

| Flag | Default | Description |
|------|---------|-------------|
| `-image` | `vxcontrol/kali-linux:latest` | Container image |
| `-cwd` | `/root` | Working directory inside the container |
| `-detach` | `false` | Run in background (no stdout/stderr capture) |
| `-timeout` | `60` | Timeout in seconds (max `10800`) |
| `-keep` | `false` | Leave the container running after the command |
| `-pull` | `false` | Always pull the image first |
| `-env` | | Path to extra `KEY=VALUE` env file |
| `-message` | | Optional note printed in the banner |

## Examples

```bash
# HTTP response headers
./url-tester -target https://example.com terminal -input 'curl -sI "$TARGET_URL"'

# Extract host and run nmap
./url-tester -target https://scanme.nmap.org terminal \
  -input 'HOST=$(echo "$TARGET_URL" | sed "s|https\?://||;s|/.*||"); nmap -sV -T4 "$HOST"'

# Long-running process (detach)
./url-tester -target https://example.com terminal \
  -input 'python3 -m http.server 8080' -detach -timeout 0

# Keep container for interactive follow-up
./url-tester -target https://example.com -keep terminal -input 'id; uname -a'
# then: docker exec -it <container-id> bash
```

## Relationship to PentAGI ftester

Original PentAGI usage (requires a running PentAGI instance):

```bash
go run cmd/ftester/main.go -flow 123 terminal -input "ls -la" -message "List files"
```

This project keeps the familiar `terminal -input ...` surface but replaces flow/container orchestration with a simple one-shot Docker run and adds the `-target` flag.

If you need the full set of tools (browser, search, agents, `describe`, etc.) run the original ftester against a live PentAGI deployment.

## License

Derived from [vxcontrol/pentagi](https://github.com/vxcontrol/pentagi). See upstream license terms for the original project. This standalone extraction is provided for convenience and team use.
