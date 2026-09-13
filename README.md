# coai coordinator

`coai` is a local, shared MCP coordination server for Claude Code, Codex CLI, and other MCP clients. Each client gets its own MCP process, while all processes share one SQLite database (WAL mode). Tasks and messages are durable; `await_task` blocks until another agent publishes a result.

## Build and release

Requires Go 1.27 or newer.

```bash
make test
make lint
make build
```

Releases are built by GoReleaser through GitHub Actions. Push a semantic version tag to publish macOS, Linux, and Windows archives:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow uses the repository's `GITHUB_TOKEN`; no personal token is required.

## Install as a plugin

Clone or install this repository as a plugin in Claude Code or Codex. Build the bundled MCP binary first:

```bash
git clone git@github.com:erwint/coai.git
cd coai
./scripts/build-plugin.sh
```

Claude Code loads the included `.mcp.json` when the plugin is installed. For Codex, install the released `coai` binary on `PATH`, then add the plugin's MCP server with `codex mcp add coai -- coai`; the same plugin manifest and coordination database are shared.

Claude Code can install it directly from the repository marketplace:

```text
/plugin marketplace add erwint/coai
/plugin install coai@erwint-tools
```

## Build and configure

```bash
go build -o coai .
codex mcp add coai -- /absolute/path/coai
claude mcp add --transport stdio coai -- /absolute/path/coai
```

Set `COAI_STATE` to share a database explicitly (the default is `$XDG_CONFIG_HOME/coai/state.json` or the platform user config directory; the filename is retained for compatibility). Keep it on a local filesystem; the database is created with private permissions.

Agents should first call `join_session` with a common session name and unique agent ID. Codex agents should then call `register_codex_thread` with their app-server `thread_id` and optional socket path. Use `create_task`, `send_message`, `ack_message`, `await_task`, `complete_task`, `claim_files`, `release_files`, and `list_session` to coordinate. `await_task` is cancellation-aware and uses durable polling, so it works across separately spawned MCP processes. When a client supplies an MCP progress token, it also receives a heartbeat before the durable wait.

## Claude Code channel notifications

The server declares Claude's experimental `claude/channel` capability and emits `notifications/claude/channel` for messages addressed to the joined agent. The Go SDK is temporarily pinned to the unreleased custom-notifications implementation from [PR #1146](https://github.com/modelcontextprotocol/go-sdk/pull/1146); remove the `replace` directive in `go.mod` when that API is released. During the research preview, run Claude with `claude --dangerously-load-development-channels server:coai`; channels require organization enablement.

## Codex app-server

For result injection, run `codex app-server daemon start`. Register the thread with `register_codex_thread`; when a task is completed for that agent, coai connects to `$CODEX_HOME/app-server-control/app-server-control.sock` (or the registered socket) and calls `thread/inject_items`. If the app-server is unavailable, the durable MCP result remains available through `await_task` and `list_session`.
