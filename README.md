# OpenGate Server

OpenGate is an OpenAI-compatible proxy server for the [`opencode`](https://opencode.ai) CLI. It lets you use any OpenAI-compatible client with `opencode` models, adding session persistence and tool-calling capabilities over a plain HTTP API.

## Features

- **OpenAI Compatibility**: Implements `/v1/models` and `/v1/chat/completions`.
- **Streaming Support**: Real-time response streaming using Server-Sent Events (SSE).
- **Session Management**: Continue previous conversations using session IDs.
- **Auto-Approval**: Dynamically skip tool execution confirmations.
- **Model Mapping**: Automatically prefixes short model names with a provider.
- **CLI Mode**: Run a single prompt without starting the HTTP server.
- **systemd Integration**: Generate or install a systemd service on Linux.

## Prerequisites

- **Go 1.26.5 or later** (to build from source).
- **`opencode`** must be installed and available in your `PATH`. OpenGate is a wrapper around the `opencode` CLI — without it nothing works. Install it with:

  ```bash
  npm install -g opencode-ai
  ```

  or follow the official installation instructions at [opencode.ai/docs](https://opencode.ai).

  > **Tip**: If `opencode` is not on `PATH` (e.g. installed via `fnm` or a custom location), point OpenGate to it with `-opencode-binary <path>` or run `-auto-config` once to resolve and persist the binary path.

## Installation

1. Clone the repository and build:

   ```bash
   git clone https://github.com/your-org/opengate.git
   cd opengate
   go build -o opengate .
   ```

2. (Optional) Verify the binary resolves the `opencode` executable:

   ```bash
   ./opengate -auto-config
   ```

3. (Optional) Install as a systemd service (requires root):

   ```bash
   sudo ./opengate -install
   ```

## Usage

Start the server:

```bash
./opengate -port 2211
```

Then point any OpenAI-compatible client at `http://localhost:2211/v1`.

### CLI Options (`-h`)

```
OpenGate OpenAI Proxy Server

Usage:
  -auto
        Auto-approve opencode tool permissions
  -auto-config
        Resolve opencode binary and node path, write ~/.opengate/config.json and exit
  -config string
        Path to config file (default: ~/.opengate/config.json)
  -continue
        Continue the last opencode session for every request
  -dir string
        Working directory for opencode sessions (default: current directory)
  -h  Show help
  -help
        Show help
  -install
        Install and start as a systemd service (requires root)
  -install-service
        Generate systemd service file
  -node-path string
        Extra PATH entry (e.g. fnm node bin dir) appended for node subprocesses
  -opencode-binary string
        Path to the opencode binary (default "opencode")
  -port string
        Port to listen on (default "2211")
  -provider string
        Default provider prefix for model IDs without a provider (default "opencode")
  -run string
        Execute a single prompt via opencode CLI and exit (no HTTP server)
  -session string
        Continue the given opencode session ID for every request
```

### Configuration File

OpenGate reads `~/.opengate/config.json` by default (override with `-config`). Flags explicitly passed on the command line take precedence over the file.

Generate the config automatically:

```bash
./opengate -auto-config
```

Example `~/.opengate/config.json`:

```json
{
  "opencodeBinary": "/home/user/.local/share/fnm/.../bin/opencode",
  "nodePath": "/home/user/.local/share/fnm/.../installation/bin",
  "workingDir": "/home/user/projects/my-app"
}
```

### HTTP Headers

You can override global settings per request using custom HTTP headers:

| Header | Value | Description |
|--------|-------|-------------|
| `X-Session-Id` | `last` | Continues the most recent session in the `sessions/` directory. |
| `X-Session-Id` | `[string]` | Uses/creates a specific session ID (e.g. `my-chat-session`). |
| `X-Auto-Approve` | `true` | Enables auto-approval for tools in this request (skips prompts). |

### Examples

#### Basic Chat Completion
```bash
curl -X POST http://localhost:2211/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "opencode/big-pickle",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

#### Continuing a Session
```bash
curl -X POST http://localhost:2211/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Session-Id: last" \
  -d '{
    "model": "opencode/big-pickle",
    "messages": [{"role": "user", "content": "What did I just say?"}]
  }'
```

#### Streaming with Auto-Approve
```bash
curl -N -X POST http://localhost:2211/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Auto-Approve: true" \
  -d '{
    "model": "opencode/big-pickle",
    "stream": true,
    "messages": [{"role": "user", "content": "Write a python script to list files."}]
  }'
```

#### Single Prompt (CLI Mode)
```bash
./opengate -run "Explain what a monad is in one sentence"
```

#### Listing Available Models
```bash
curl http://localhost:2211/v1/models
```

### Model Names

Models are referenced as `provider/model` (e.g. `opencode/big-pickle`, `google-vertex/gemini-2.5-flash`, `openai/gpt-5.1`). Short names without a provider are prefixed with the value of `-provider` (default: `opencode`), so you can send either `deepseek-v4-flash-free` or `opencode/deepseek-v4-flash-free`.

The list advertised by `/v1/models` comes from `models.json` in the working directory — edit it to expose the models you actually use.

### systemd Service

Two installation modes are available on Linux:

- `-install` — writes the unit to `/etc/systemd/system/opengate.service`, then enables and starts it (requires `sudo`).
- `-install-service` — only generates the unit at `/tmp/opengate.service` and prints the `systemctl` commands for you to run manually.

## Troubleshooting

- **`opencode: executable file not found`** — `opencode` is not installed or not on `PATH`. Install it (see [Prerequisites](#prerequisites)) or pass `-opencode-binary /full/path/to/opencode`.
- **Model not available** — check that the model ID is listed in `models.json` and that your `opencode` installation has the corresponding provider configured (`opencode models` / `opencode auth`).
- **Streaming stuck or empty** — make sure your client sends `"stream": true` and that no proxy between client and server buffers the response.

## Testing

A quick smoke test script is included. Start the server, then run:

```bash
./test.sh
```

## License

MIT
