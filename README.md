# OpenGate Server

OpenGate is an OpenAI-compatible proxy server for the `opencode` CLI. It allows you to use your favorite AI clients with `opencode` models, providing session persistence and tool-calling capabilities over HTTP.

## Features

- **OpenAI Compatibility**: Supports `/v1/models` and `/v1/chat/completions`.
- **Streaming Support**: Real-time response streaming using Server-Sent Events (SSE).
- **Session Management**: Continue previous conversations using session IDs.
- **Auto-Approval**: Dynamically skip tool execution confirmations.
- **Model Mapping**: Automatically maps short names to providers.

## Installation

1. Ensure you have Go installed (1.26.5 or later).
2. Build the project:
   ```bash
   go build -o opengate .
   ```
3. (Optional) Install as a systemd service:
   ```bash
   ./opengate -install-service
   ```

## Usage

Start the server:
```bash
./opengate -port 2211
```

### Configuration Flags

- `-port`: Port to listen on (default: 2211).
- `-dir`: Working directory for opencode sessions.
- `-auto`: Global auto-approve for tool permissions.
- `-continue`: Always continue the last session.
- `-session`: Always use a specific session ID.
- `-opencode-binary`: Path to the `opencode` executable.

### HTTP Headers

You can override global settings per request using custom HTTP headers:

| Header | Value | Description |
|--------|-------|-------------|
| `X-Session-Id` | `last` | Continues the most recent session in the `sessions/` directory. |
| `X-Session-Id` | `[string]` | Uses/Creates a specific session ID (e.g., `my-chat-session`). |
| `X-Auto-Approve`| `true` | Enables auto-approval for tools in this request (skips prompts). |

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

## License

MIT
