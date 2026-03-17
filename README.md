# Cross Guard - Mattermost Plugin

Cross Guard plugin for Mattermost Federal. Enables cross-domain message relay between Mattermost servers via NATS.

## Development

### Prerequisites

- Go 1.24+
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
| `/crossguard init-team` | Initialize Cross Guard for the current team (requires team or system admin) |
| `/crossguard init-channel` | Enable relay for the current channel (requires channel admin or higher) |
| `/crossguard teardown-channel` | Disable relay for the current channel |
| `/crossguard teardown-team` | Remove Cross Guard from the current team |
| `/crossguard status` | Show initialization status for the current team |

Typical workflow: `init-team` first, then `init-channel` on each channel you want relayed.

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
