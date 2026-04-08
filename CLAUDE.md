# Cross Guard - Mattermost Plugin

## Critical Workflow

**Research -> Plan -> Implement** -- never jump straight to coding.

1. **Research**: Explore the codebase, understand existing patterns
2. **Plan**: Create a detailed implementation plan and verify it with the user
3. **Implement**: Execute the plan with validation checkpoints

When asked to implement any feature, first say: "Let me research the codebase and create a plan before implementing."

## Use Multiple Agents

Leverage subagents aggressively for better results:

- Spawn agents to explore different parts of the codebase in parallel
- Use one agent to write tests while another implements features
- Delegate research tasks
- For complex refactors: one agent identifies changes, another implements them

## Working Memory Management

When context gets long:

- Re-read CLAUDE.md files to refresh context
- Summarize progress before major changes
- Document current state before large refactors

## Problem-Solving Approach

When stuck or confused:

1. **Stop** -- don't spiral into complex solutions
2. **Delegate** -- consider spawning agents for parallel investigation
3. **Step back** -- re-read the requirements
4. **Simplify** -- the simple solution is usually correct
5. **Ask** -- "I see two approaches: [A] vs [B]. Which do you prefer?"

## Formatting

- Never use em dashes in code, comments, strings, or commit messages. Use commas, periods, or parentheses instead.

## Important Development Notes

- Always run checks (`make check-style`, `make test`) before committing code
- When in doubt, choose clarity over cleverness
- Avoid complex abstractions or "clever" code
- Do not add verbose comments; only comment complex operations
- Never fabricate commands that don't exist
- Never add synthetic or mock data to fix failing tests

## Absolute Git Prohibitions

These commands destroy uncommitted work and are **forbidden**:

- **Never run `git checkout -- <path>`**
- **Never run `git checkout HEAD -- <path>`**
- **Never run `git restore <path>`**
- **Never run `git reset --hard`**

### What To Do Instead

```bash
# CORRECT WAY:
git stash                    # Save changes safely
# ... run tests ...
git stash pop                # Restore changes
```

## Code Analysis Standards

Before raising concerns, trace actual execution paths and construct specific failing scenarios. Focus on evidence-based claims with concrete code examples rather than theoretical issues.

## Project Overview

Cross Guard is a Mattermost Federal plugin built on the mattermost-plugin-starter-template. It provides a Go backend with a React/TypeScript frontend.

- **Plugin ID**: `crossguard`
- **Min Mattermost Version**: 6.2.1

## Reference Documentation

- [Mattermost Plugin Development](https://developers.mattermost.com/integrate/plugins/)
- [Webapp API Reference](https://developers.mattermost.com/integrate/reference/webapp/webapp-reference/)
- [Plugin Starter Template](https://github.com/mattermost/mattermost-plugin-starter-template)
- [Plugin Overview](https://developers.mattermost.com/integrate/plugins/overview/)
- [Demo Plugin](https://github.com/mattermost/mattermost-plugin-demo)

## Auto-Generated Files

- `server/manifest.go` -- Generated from `plugin.json`. Do not edit manually.
- `webapp/src/manifest.ts` -- Generated from `plugin.json`. Do not edit manually.

## Key Files

- `plugin.json` - Plugin manifest (ID, version, min server version)
- `go.mod` - Go dependencies
- `Makefile` - Build commands
- `.golangci.yml` - Go linter configuration

## Docker Development Environment (Dual-Server)

The dev environment runs two Mattermost servers with a shared NATS bus.

### Getting Started

```bash
make hosts-setup    # Add low.test and high.test to /etc/hosts (one-time, requires sudo)
make docker-setup   # Start containers, create users and teams
make deploy         # Build and deploy plugin to both servers
```

After setup:

- **Server A (Low)**: http://low.test:8075 (admin/password, usera/password, Team: Test A)
- **Server B (High)**: http://high.test:8076 (admin/password, userb/password, Team: Test B)
- **NATS**: nats://localhost:4222 (monitor: http://localhost:8222)
- **NATS (from plugins)**: nats://nats:4222

### Common Commands

| Command | Description |
|---------|-------------|
| `make hosts-setup` | Add low.test/high.test to /etc/hosts (requires sudo) |
| `make docker-setup` | First-time setup: start containers, create users and teams |
| `make deploy` | Build and deploy plugin to both Docker servers |
| `make dist` | Build plugin bundle only |
| `make test` | Run all tests |
| `make check-style` | Lint code |
| `make nuke` | Remove everything: containers, data, build artifacts |

### Docker Management Commands

| Command | Description |
|---------|-------------|
| `make docker-start` | Start containers (without user setup) |
| `make docker-stop` | Stop containers (preserves data) |
| `make docker-down` | Stop and remove containers |
| `make docker-clean` | Remove containers and all data |
| `make docker-logs` | Follow Server A logs |
| `make docker-logs-b` | Follow Server B logs |
| `make docker-reset` | Disable and re-enable plugin on both servers |
| `make docker-smoke-test` | Run end-to-end relay smoke test (init, post, verify) |
| `make docker-disable` | Disable plugin on both servers |
| `make docker-enable` | Enable plugin on both servers |
| `make docker-plugin-list` | List installed plugins on both servers |
| `make docker-kill-orphans` | Kill orphaned containers on MM ports |

### Release and Security

| Command | Description |
|---------|-------------|
| `make release` | Full release: checks, tests, SBOM audit, CodeQL, sign, checksum |
| `make release-tag` | Create git tag for the current version |
| `make sbom-audit` | Generate SBOMs and scan for vulnerabilities |
| `make codeql-analyze` | Run CodeQL security analysis on all code |
| `make security-gate` | Check scan results for critical/high issues |

## Technology Stack

### Backend
- Go 1.26
- Mattermost Plugin API
- Gorilla Mux (routing)

### Frontend
- React 18.2
- TypeScript 5.9
- Redux 5.0
- Webpack 5.105
- Mattermost Redux

### Testing
- Go: `stretchr/testify`
- Frontend: Playwright

## Go Files

After editing Go files, run `make check-style` to fix import formatting.
