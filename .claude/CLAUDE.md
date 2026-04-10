# Project Guidelines

## Overview
Cross Guard is a Mattermost Federal plugin that enables cross-domain message relay capabilities.

## Architecture
- `server/` - Go backend (plugin API, slash commands, configuration, message providers, store layer, model definitions)
- `webapp/` - React/TypeScript frontend (admin console components)
- `build/` - Build tooling (manifest generation, deployment scripts)
- `docker/` - Docker compose config and plugin volumes
- `schema/` - Data schemas (XSD for cross-domain solutions)
- `implementation-plans/` - Feature planning documents
- `plugin.json` - Plugin manifest

## Coding Conventions
- Follow existing patterns in the codebase
- Match the style of surrounding code
- Use meaningful variable and function names
- Keep functions focused and small

## Testing
- Write tests for new functionality
- Run existing tests before submitting changes
- Aim for meaningful coverage of critical paths

## Git Workflow
- Create feature branches for new work
- Write clear, descriptive commit messages
- Keep commits focused on a single change

## Error Handling
- Handle errors explicitly, don't ignore them
- Provide context when wrapping errors
- Log errors at appropriate levels

## Dependencies
- Prefer standard library solutions when available
- Evaluate dependencies for maintenance and security
- Keep dependencies up to date
