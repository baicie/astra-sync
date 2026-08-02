# Contributing to AstraSync

Thank you for your interest in contributing to AstraSync!

## Getting Started

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR_USERNAME/astra-sync.git
   cd astrasync
   ```
3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/astrasync/astra-sync.git
   ```

## Development Setup

### Prerequisites

- Java 21+
- Maven 3.9+
- Go 1.22+
- Docker & Docker Compose

### Building

```bash
# Build all modules
mvn clean package

# Build with tests
mvn clean test

# Build with integration tests
mvn clean verify
```

### Code Style

We use Spotless for code formatting:

```bash
# Check formatting
mvn spotless:check

# Apply formatting
mvn spotless:apply
```

## Branching Strategy

- `main`: Stable release branch
- `develop`: Development branch
- `feature/*`: Feature branches
- `fix/*`: Bug fix branches
- `release/*`: Release branches

## Commit Messages

A commit-msg hook is included in `.githooks/commit-msg`. Install it once after cloning:

```bash
./scripts/install-git-hooks.sh
```

On PowerShell:

```powershell
./scripts/install-git-hooks.ps1
```

Commits follow Conventional Commits:

```
type(scope): lowercase imperative subject

[optional body]

[optional footer]
```

Allowed types are `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, and `test`. Keep the subject under 100 characters. Add `!` after the type or scope for breaking changes, for example `feat(protocol)!: change frame envelope`.

Use the repository template with:

```bash
git config commit.template .gitmessage
```

## Pull Request Process

1. Create a feature branch from `develop`
2. Make your changes
3. Add tests for new functionality
4. Ensure all tests pass
5. Update documentation if needed
6. Submit a pull request

### PR Checklist

- [ ] Code follows project style guidelines
- [ ] Code is properly tested
- [ ] Documentation is updated
- [ ] Commit messages are clear
- [ ] No merge conflicts with `develop`

## Code of Conduct

Please be respectful and constructive in all interactions.
We follow the [Contributor Covenant](https://www.contributor-covenant.org/).

## Questions?

- Open an issue for bugs
- Start a discussion for questions
- Join our community channels

## License

By contributing, you agree that your contributions will be licensed under Apache License 2.0.
