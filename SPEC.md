# ACOO - Agent Command Orchestrator

## Overview

ACOO is a CLI that runs multiple AI agents in parallel, each with their own scheduled jobs. Jobs use LLM providers (via fantasy) to process tasks.

## File Structure

```
~/.config/acoo/
├── agents/
│   └── {name}.md        # Agent definitions
└── jobs/
    └── {name}.md        # Job definitions
```

## Agent Definition (`agents/{name}.md`)

```markdown
---
env:
  GITHUB_TOKEN: abc123
jobs:
  review-changes: "@every 30s"
  summarize-reviews: "0 9 * * *"
---

You are a code reviewer. You review proposed code changes for clarity and correctness.
```

### Front Matter Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `env` | map | No | Environment variables (literal values) |
| `jobs` | map | Yes | Job name → schedule mapping |

### Body

The markdown body (after front matter) is the **system prompt** for the agent.

## Job Definition (`jobs/{name}.md`)

```markdown
---
provider: openai
model: gpt-4o-mini
thinking: low
preconditions:
  - "command -v changes_to_review >/dev/null"
env:
  REPO_PATH: /home/user/repo
---

Run the script 'changes_to_review' which outputs a list of changes. Review each change and send an email with the results.
```

### Front Matter Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | string | Yes | Provider name (`openai`, `anthropic`, `minimax`, etc.) |
| `model` | string | Yes | Model ID (e.g., `gpt-4o`, `claude-3-5-sonnet`) |
| `thinking` | string/int | No | Thinking budget (effort level or token count) |
| `preconditions` | list | No | Shell commands that must pass before job runs |
| `env` | map | No | Job-specific environment variables |
| `compaction.retain_tokens` | int | No | Tokens to retain during compaction (default: 20000) |

### Thinking Budget

Named effort levels or numeric tokens:

| Level | Tokens |
|-------|--------|
| `disabled` | 0 |
| `low` | 10,000 |
| `medium` | 16,000 |
| `high` | 32,000 |
| `very_high` | 64,000 |
| `max` | 100,000 |

Or specify directly: `thinking: 20000`

### Body

The markdown body is the **task prompt** - the instruction/task for the agent.

## Schedule Format

Each job has its own explicit schedule:

- **Cron**: `0 0 9 * * *` (6 fields with seconds: sec min hour day month dow)
- **Interval**: `@every 30s`, `@every 5m`, `@every 1h` (seconds supported)
- **One-shot**: `@once`

## Execution Flow

```
┌─────────────────────────────────────────────────────────┐
│ 1. Load all agents and jobs                           │
│ 2. For each agent:                                    │
│    a. Create runner with system prompt                │
│    b. For each job:                                   │
│       - Start scheduler goroutine                     │
│ 3. On schedule trigger:                               │
│    a. Check preconditions                             │
│    b. Load session state from pebble store            │
│    c. If all pass: spawn subprocess with env vars     │
│    d. Subprocess: fantasy agent loop                  │
│    e. Save session state after each turn              │
│    f. Continue until '<<<<<DONE>>>>>'                │
│ 4. Hot reload on file changes                         │
└─────────────────────────────────────────────────────────┘
```

## Session Persistence

Sessions persist between job executions using [Pebble](https://github.com/cockroachdb/pebble) storage. State is stored in:

```
~/.local/state/acoo/
├── daemon.sock     # IPC socket for storage operations
└── state/
    └── 00000.sst, 00001.log, ...
```

### Architecture

The daemon process owns the Pebble store and exposes a Unix socket (`daemon.sock`) for IPC. The web UI and agent subprocesses communicate with the daemon via JSON messages over this socket.

### Key Design

Messages are stored as **individual keys** (append-only):
```
msg:agent:{timestamp_nano} → {"role": "user", "content": "Hello", ...}
msg:agent:1234567890   → assistant message
msg:agent:1234567891   → user message
```

Benefits:
- O(1) append for new messages (just one key per turn)
- O(n) load for full history (rare, only at compaction)
- Compacted messages marked in place, not rewritten
- Compaction history stored separately as JSON array

### Data Stored

- `msg:agent:{ts}` — Individual messages (one key per message). Full content including tool calls/results.
- `sysprompt:agent` — Current system prompt. Previous prompts are kept with `replaced: true` if the prompt changes. The web UI shows whether the prompt is current or replaced.
- `compact:agent` — Compaction history (JSON array)
- `meta:agent` — Metadata (message count, compaction count)

## Done Marker

Agents signal completion by including `<<<<<DONE>>>>>` alone on its own line. The system will continue looping until this marker appears.

## Tools

Agents have access to the following tools:

| Tool | Description |
|------|-------------|
| `read_file` | Read the contents of a file |
| `edit_file` | Write or append content to a file |
| `bash` | Run a shell command |
| `glob` | List files matching a glob pattern |
| `list_dir` | List contents of a directory |

### read_file

```json
{"path": "/path/to/file.txt"}
```

### edit_file

```json
{"path": "/path/to/file.txt", "content": "Hello, world!", "append": false}
```

### bash

```json
{"command": "ls -la", "timeout": 30}
```

### glob

```json
{"pattern": "*.go", "dir": "./src"}
```

### list_dir

```json
{"path": "/home/user/projects"}
```

## Environment Variables

Environment variables are merged from agent and job levels:
- Agent env vars are set first
- Job env vars override agent vars with same name

All values are literal - no `$VAR` substitution.

## Hot Reloading

ACOO watches the agents and jobs directories:
- **Add agent** → automatically starts new runner
- **Remove agent** → automatically stops runner
- **Modify config** → reloads that agent

## Compaction

ACOO automatically handles context overflow by compacting (summarizing) older conversation history. This prevents LLM context window errors during long-running agent sessions.

### How It Works

1. **Detection**: When the conversation approaches the context limit (80% of retain budget), compaction is triggered proactively, or when a context overflow error occurs.

2. **Retention**: Recent messages (default: 20k tokens worth) are kept intact. The system message is always retained regardless of token budget. Older messages are summarized.

3. **System Message**: Always uses the current agent definition. Stored messages do not include the system prompt; it is prepended at load time from the latest `agents/{name}.md` file.

3. **Summarization**: The LLM generates a structured summary of older messages including:
   - **Goal**: What the agent was trying to accomplish
   - **Progress**: What has been completed
   - **Key Decisions**: Important choices made
   - **Current State**: Where work stands

4. **Storage**: Old messages are marked as `compacted: true` in place (no rewriting). A compaction record with the summary is stored separately.

5. **Tool Call Handling**: Tool results are truncated (first 500 + last 200 chars) to prevent large outputs from consuming context.

### Configuration

Compaction is always enabled. You can customize the retain budget in job config:

```yaml
---
provider: openai
model: gpt-4o-mini
compaction:
  retain_tokens: 30000  # Tokens to retain (default: 20000)
---

Your task here.
```

### Safety Limits

- Maximum 2 compaction attempts per job to prevent infinite loops
- Falls back to simple pruning (keep recent N messages) if compaction fails

## Skip-if-Running

If a job is still running when its next trigger fires, the trigger is skipped (not queued).

## CLI Commands

```bash
acoo                    # Run all agents
acoo run                # Run all agents (same as above)
acoo list               # List all agents and their jobs
acoo validate           # Validate all configs
acoo test <name> <job>  # Run a job once (with preconditions)
acoo providers          # List available providers and models
acoo --serve           # Start web UI at localhost:8080
acoo --serve --serve-addr :9090  # Start web UI at custom address
```

## Web UI

Run `acoo --serve` to start a web UI at `http://localhost:8080`. Use `--serve-addr` to specify a custom address.

Features:
- List all agents with session stats
- View agent session history
- See compaction history with summaries
- View all messages including compacted ones

## Project Structure

```
cmd/acoo/
  main.go           # Entry point, CLI, agent manager
  commands.go       # CLI command implementations
  cli.go            # CLI flag definitions
  subprocess.go     # Subprocess entry for job execution
  wizard.go         # TUI for creating agents
  web.go            # Web UI server
  daemon/
    ipc.go          # IPC server and client for storage operations
  templates/
    index.html      # Agent list page
    agent.html      # Agent detail page

internal/
  config/
    types.go        # Shared types (ThinkingBudgets)
    agent.go        # Agent config types
    job.go          # Job config types
    loader.go       # File loading and parsing
    watcher.go      # File system watcher for hot reload
  agent/
    runner.go       # Agent runner (goroutine per job)
    tools.go        # Tool definitions
    compactor.go    # Context compaction logic
  scheduler/
    scheduler.go    # Cron/interval parsing
  provider/
    factory.go      # LLM provider factory
  storage/
    store.go        # Pebble-based session persistence
```

## Dependencies

- `charm.land/fantasy` - AI agent framework
- `charm.land/catwalk` - Model registry
- `github.com/spf13/cobra` - CLI framework
- `github.com/robfig/cron/v3` - Cron parsing
- `github.com/fsnotify/fsnotify` - File watching
- `github.com/cockroachdb/pebble` - Session state persistence
