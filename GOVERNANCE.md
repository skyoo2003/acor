# ACOR Governance

## Project Lead

- **Sungkyu Yoo** ([@skyoo2003](https://github.com/skyoo2003)) — Creator and maintainer

## Decision Making

ACOR follows a **BDFL (Benevolent Dictator for Life)** governance model. The project lead makes final decisions on:
- Architecture and design
- Feature acceptance and prioritization
- Release scheduling
- Code of Conduct enforcement

## Contribution Model

ACOR accepts contributions from the community. All contributions are reviewed by the project lead.

- Small changes (typos, docs fixes) are merged quickly
- Feature additions require discussion via [GitHub Issues](https://github.com/skyoo2003/acor/issues) or [Discussions](https://github.com/skyoo2003/acor/discussions) before implementation
- Breaking changes require a dedicated issue with migration guide

## Release Process

ACOR follows [Semantic Versioning](https://semver.org/). What a major version actually
promises — which surfaces are covered, which are excluded, and what counts as a breaking
change — is defined in the [compatibility policy](docs/content/reference/compatibility.md).
That page is the single source; these rules are not restated here.

The decision to cut a major version is the project lead's.

Releases are managed via [Changie](https://github.com/miniscruff/changie) for changelog generation.

## Maintainership

New maintainers are added by the project lead based on:
- Sustained, high-quality contributions
- Understanding of the codebase and architecture
- Alignment with the project's direction

There is no formal term limit. Maintainership is a role, not a title.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). Enforcement is the responsibility of the project lead.

## License

ACOR is licensed under the [Apache License 2.0](LICENSE). All contributions must be compatible with this license.
