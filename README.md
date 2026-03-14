# Cross Guard - Mattermost Plugin

Cross Guard plugin for Mattermost Federal.

## Development

### Prerequisites

- Go 1.24+
- Node.js 20+
- Docker (for local development environment)

### Quick Start

```bash
# Install dependencies and build
cd webapp && npm install && cd ..
make dist

# Docker development environment
make docker-setup
make deploy
```

### Common Commands

| Command | Description |
|---------|-------------|
| `make docker-setup` | First-time setup: start containers, create admin user |
| `make deploy` | Build and deploy plugin to Docker Mattermost |
| `make dist` | Build plugin bundle only |
| `make test` | Run all tests |
| `make check-style` | Lint code |
| `make clean` | Remove build artifacts |

### Release

```bash
make release
make release-tag
git push origin v$(VERSION)
```
