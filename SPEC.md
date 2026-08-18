# ACOO - Agent Command Orchestrator

## Overview

ACOO is a CLI that runs multiple AI agents in parallel, each with their own schedules and jobs. Agents use LLM providers (via fantasy) to process tasks defined in job files.

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
model: gpt-4o
provider: openai
env:
  GITHUB_TOKEN: abc123
  EMAIL_FROM: noreply@example.com
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
| `model` | string | Yes | Model ID (e.g., `gpt-4o`, `claude-3-5-sonnet`) |
| `provider` | string | Yes | Provider name (`openai`, `anthropic`, `openrouter`, etc.) |
| `env` | map | No | Environment variables (literal values) |
| `jobs` | map | Yes | Job name → schedule mapping |

### Body

The markdown body (after front matter) is the **system prompt** for the agent.

## Job Definition (`jobs/{name}.md`)

```markdown
---
name: review-changes
---

Run the script 'changes_to_review' which outputs a list of changes. Review each change and send an email with the results to the address returned by the command.
```

### Front Matter Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Job identifier |

### Body

The markdown body is the **task prompt** - the instruction/task for the agent.

## Schedule Format

Each job has its own explicit schedule:

- **Cron**: `0 0 * * * *` (6 fields with seconds: sec min hour day month dow)
- **Interval**: `@every 30s`, `@every 5m`, `@every 1h`
- **One-shot**: `@once`

Seconds are supported in `@every` intervals.

## Execution Flow

```
┌─────────────────────────────────────────────────────────┐
│ 1. Load all agents and jobs                           │
│ 2. For each agent:                                    │
│    a. Create fantasy.Agent with system prompt         │
│    b. For each job:                                   │
│       - Start scheduler goroutine                      │
│ 3. On schedule trigger:                               │
│    a. Combine system prompt + job prompt              │
│    b. Call fantasy agent in a loop                    │
│    c. Continue until '<<<<<DONE>>>>>'                │
│    d. Return final response                           │
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

## Hot Reloading

ACOO watches the agents and jobs directories:
- **Add agent** → automatically starts new runner
- **Remove agent** → automatically stops runner
- **Modify config** → reloads that agent

## Environment Isolation

Each agent runs in its own subprocess with isolated environment:
- Agent-specific environment variables are isolated
- Each agent gets its own LLM process
- Prevents cross-contamination between agents

## Supported Providers

- `openai` - OpenAI models
- `anthropic` - Anthropic models
- `openrouter` - OpenRouter aggregated models
- `google` / `gemini` - Google AI models
- Any OpenAI-compatible endpoint (URL)

## CLI Commands

```bash
acoo                    # Run all agents
acoo run                # Run all agents (same as above)
acoo run <name>         # Run a specific agent once
acoo list               # List all agents and their jobs
acoo validate           # Validate all configs
acoo test <name> <job>  # Test an agent job (show prompts, dry run)
```

## Project Structure

```
cmd/acoo/
  main.go           # Entry point, CLI, agent manager

internal/
  config/
    agent.go       # Agent config types
    job.go         # Job config types
    loader.go      # File loading and parsing
    watcher.go     # File system watcher for hot reload
  agent/
    runner.go      # Agent runner (goroutine per job)
  scheduler/
    scheduler.go   # Cron/interval parsing
  provider/
    factory.go     # LLM provider factory
```

## Dependencies

- `charm.land/fantasy` - AI agent framework
- `charm.land/catwalk` - Model registry
- `github.com/spf13/cobra` - CLI framework
- `github.com/robfig/cron/v3` - Cron parsing
- `github.com/fsnotify/fsnotify` - File watching
