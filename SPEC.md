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
name: code-reviewer
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
| `name` | string | Yes | Agent identifier |
| `env` | map | No | Environment variables (literal values) |
| `jobs` | map | Yes | Job name → schedule mapping |

### Body

The markdown body (after front matter) is the **system prompt** for the agent.

## Job Definition (`jobs/{name}.md`)

```markdown
---
name: review-changes
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
| `name` | string | Yes | Job identifier |
| `provider` | string | Yes | Provider name (`openai`, `anthropic`, `minimax`, etc.) |
| `model` | string | Yes | Model ID (e.g., `gpt-4o`, `claude-3-5-sonnet`) |
| `thinking` | string/int | No | Thinking budget (effort level or token count) |
| `preconditions` | list | No | Shell commands that must pass before job runs |
| `env` | map | No | Job-specific environment variables |

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
│    b. If all pass: spawn subprocess with env vars     │
│    c. Subprocess: fantasy agent loop                  │
│    d. Continue until '<<<<<DONE>>>>>'                │
│ 4. Hot reload on file changes                         │
└─────────────────────────────────────────────────────────┘
```

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
```

## Project Structure

```
cmd/acoo/
  main.go           # Entry point, CLI, agent manager
  subprocess.go     # Subprocess entry for job execution
  wizard.go         # TUI for creating agents

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
    executor.go     # Job execution logic
  scheduler/
    scheduler.go    # Cron/interval parsing
  provider/
    factory.go      # LLM provider factory
```

## Dependencies

- `charm.land/fantasy` - AI agent framework
- `charm.land/catwalk` - Model registry
- `github.com/spf13/cobra` - CLI framework
- `github.com/robfig/cron/v3` - Cron parsing
- `github.com/fsnotify/fsnotify` - File watching
