# Cross Guard - Mattermost Plugin

Cross Guard plugin for Mattermost Federal. Enables cross-domain message relay between Mattermost servers via NATS.

## Development

### Prerequisites

- Go 1.26+
- Node.js 20+
- Docker (for local development environment)

### Quick Start

```bash
# Set up /etc/hosts for dual-server hostnames
make hosts-setup

# Install dependencies and build
cd webapp && npm install && cd ..
make dist

# Docker development environment (dual Mattermost servers + NATS)
make docker-setup
make deploy
```

### Docker Development Environment (Dual-Server)

The dev environment runs two Mattermost servers (A and B) with a shared NATS bus for cross-domain relay testing.

After `make docker-setup`:

- **Server A (Low)**: http://low.test:8075
  - Admin: `admin / password`
  - User: `usera / password`
  - Team: Test A
- **Server B (High)**: http://high.test:8076
  - Admin: `admin / password`
  - User: `userb / password`
  - Team: Test B
- **NATS**: `nats://localhost:4222` (monitor: http://localhost:8222)

`make deploy` automatically configures Server A with an outbound NATS connection and Server B with an inbound NATS connection.

### Slash Commands

Once the plugin is deployed, use `/crossguard` to manage cross-domain relay:

| Command | Description |
|---------|-------------|
| `/crossguard init-team [connection-name]` | Link a NATS connection to this team (requires team admin or system admin) |
| `/crossguard init-channel [connection-name]` | Link a NATS connection to this channel (requires channel admin or higher) |
| `/crossguard teardown-team [connection-name]` | Unlink a NATS connection from this team (requires team admin or system admin) |
| `/crossguard teardown-channel [connection-name]` | Unlink a NATS connection from this channel (requires channel admin or higher) |
| `/crossguard reset-prompt <connection-name>` | Clear a pending team connection prompt (requires team admin) |
| `/crossguard reset-channel-prompt <connection-name>` | Clear a pending channel connection prompt (requires team admin) |
| `/crossguard rewrite-team [name] [team]` | Set or clear a remote team name rewrite for an inbound connection (requires team admin) |
| `/crossguard status` | Show Cross Guard status for this team |
| `/crossguard help` | Show detailed help for all Cross Guard commands |

Typical workflow: `init-team <connection-name>` first, then `init-channel <connection-name>` on each channel you want relayed.

### Common Commands

| Command | Description |
|---------|-------------|
| `make hosts-setup` | Add `low.test` and `high.test` to /etc/hosts (requires sudo) |
| `make docker-setup` | First-time setup: start containers, create users and teams |
| `make deploy` | Build and deploy plugin to both Docker servers |
| `make dist` | Build plugin bundle only |
| `make test` | Run all tests |
| `make check-style` | Lint code |
| `make clean` | Remove build artifacts |
| `make nuke` | Remove everything: containers, data, build artifacts |

### Docker Management Commands

| Command | Description |
|---------|-------------|
| `make docker-start` | Start containers (without setup) |
| `make docker-stop` | Stop containers (preserves data) |
| `make docker-down` | Stop and remove containers |
| `make docker-clean` | Remove containers and all data |
| `make docker-logs` | Follow Server A logs |
| `make docker-logs-b` | Follow Server B logs |
| `make docker-reset` | Disable and re-enable plugin on both servers |
| `make docker-disable` | Disable plugin on both servers |
| `make docker-enable` | Enable plugin on both servers |
| `make docker-plugin-list` | List installed plugins on both servers |
| `make docker-smoke-test` | Run end-to-end relay smoke test (init, post, verify) |
| `make docker-kill-orphans` | Kill orphaned containers on MM ports |

### Release

```bash
make release        # Full build: checks, tests, SBOM audit, CodeQL, sign, checksum
make release-tag    # Create git tag for the version
git push origin v$(PLUGIN_VERSION)
```

### Security Scanning

| Command | Description |
|---------|-------------|
| `make sbom` | Generate SBOMs (CycloneDX) for Go and Node.js |
| `make sbom-audit` | Generate SBOMs and scan for vulnerabilities |
| `make codeql-analyze` | Run CodeQL on Go and JavaScript/TypeScript |
| `make security-gate` | Check scan results for critical/high issues |
