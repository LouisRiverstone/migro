# Changelog

All notable changes are listed here. The project is pre-1.0: minor versions may contain breaking changes, and they are called out under **Breaking**.

## [Unreleased]

## [0.2.0] - 2026-11-17

### Added
- `migro make:model Name --from-table=users` generates a model from a live table.
- `migro --version`.
- `Query.GroupByRaw` for grouping by an expression.
- Runnable `Example*` tests for the godoc, and docs: coming from GORM, coming from Laravel/Lucid, per-database feature table.
- Tests that fix the eager-loading query count and check that identifier arguments stay quoted.
- `SECURITY.md`, issue templates, Dependabot.
- CI jobs for MariaDB, CockroachDB and SQL Server (non-blocking until verified), `govulncheck`, coverage report.
- `-race` on the PostgreSQL and MySQL jobs.
- Tagged releases publish CLI binaries (linux, macOS, windows; amd64, arm64) through GoReleaser.

### Fixed
- **Security:** `Query.GroupBy` sent its argument unquoted, so a caller-supplied value could inject SQL. It is now quoted like `OrderBy`. Use `GroupByRaw` for expressions. **Breaking** for code that passed an expression to `GroupBy`.

## [0.1.1]

- Documentation: version picker, per-release docs build, first-release install notes.

## [0.1.0]

- Models, relations, collections, casts, resources, scopes, strict mode, lifecycle, UUID/ULID keys.
- Schema builder, query builder, migrations, schema dump and diff, seeders and factories.
- Events, read replicas, sharding, and the `migro` CLI.
