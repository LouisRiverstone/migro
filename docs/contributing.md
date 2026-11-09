---
title: Contributing
nav_order: 20
description: How to set up, test and open a pull request for migro.
---

# Contributing

Thanks for helping. This page covers everything from setting up to getting a pull request merged. If something here is wrong or unclear, that is a bug worth a PR too.

## Ways to help

- **Report a bug.** Open an issue with what you ran, what you expected, what happened, your Go version, database and version.
- **Fix a bug or build a feature.** For anything bigger than a small fix, open an issue first so we agree on the approach before you write code.
- **Verify a database.** PostgreSQL and MySQL run in CI. CockroachDB, Redshift, SQL Server, Oracle and MariaDB are generated and unit-tested but have never been run against a live server. Running the suite on one of them and reporting or fixing what breaks is one of the most valuable things you can do (see [Running tests against a real database](#running-tests-against-a-real-database)).
- **Improve the docs.** Fixes, missing examples and clearer wording are all welcome (see [Documentation](#documentation)).
- **Security problems.** Do not open a public issue. Use the repository's **Security** tab and choose *Report a vulnerability* (private reporting), or contact the maintainer directly.

Be kind and assume good intent. Disagree with the code, not the person.

## Set up

You need Go 1.25 or newer and Git. Nothing else is required to run the default test suite.

```
git clone https://github.com/<you>/migro.git     # your fork
cd migro
git remote add upstream https://github.com/LouisRiverstone/migro.git
git checkout -b feat/my-change
go test ./...
```

Before every push, these three must pass cleanly (CI runs them):

```
gofmt -l .          # prints nothing when formatting is right
go vet ./...
go test -race -count=1 ./...
```

## Repository layout

| Path | What lives there |
|---|---|
| `migro.go`, `pool.go`, `errors.go` | `Open`, `DB`, pool and replica handling, the `*Error` type and its codes |
| `dialect/` | The `Dialect` interface and registry; one sub-package per database family |
| `schema/` | Table and column builder and the per-dialect DDL compiler |
| `query/` | Fluent query builder for tables without a model |
| `orm/` | Generic models; `relations.go` has the relations GORM lacks |
| `migrate/` | Registry, migrator, pretend mode; `diff/` generates migrations from models |
| `seed/` | Factories, faker, seeders |
| `events/` | Event bus and the GORM plugin |
| `cluster/` | Sharding |
| `console/` | The command implementations (`commands.go`, `make.go`, stubs) |
| `cmd/migro/` | The `migro` program and the launcher that builds the project runner |
| `integration/` | End-to-end suite: Go migrations, class seeders, factories and Eloquent/Lucid-style models for a whole database (indexes, 1:1, 1:N, N:M, polymorphic, self-referencing) |
| `internal/testdb/` | Opens the database the tests run against |
| `docs/` | This documentation site |

## Running tests against a real database

By default tests run on a temporary SQLite file. Set two environment variables to run the **same suite** against another database:

```
MIGRO_TEST_DIALECT=postgres
MIGRO_TEST_DSN="host=localhost user=postgres password=postgres dbname=migro port=5432 sslmode=disable"
go test -p 1 -count=1 ./...
```

{: .warning }
When these variables are set, the tests **drop every table** in that database, at the start and the end of each test. Point them at a throwaway database, never one with data you care about. `-p 1` is required because packages share the database.

Throwaway databases with Docker, matching CI:

```
# PostgreSQL
docker run -d --name migro-pg -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=migro -p 5432:5432 postgres:16

# MySQL
docker run -d --name migro-mysql -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=migro -p 3306:3306 mysql:8
# MIGRO_TEST_DIALECT=mysql
# MIGRO_TEST_DSN="root:root@tcp(127.0.0.1:3306)/migro?parseTime=true"
```

`internal/testdb` only imports the `postgres`, `mysql` and `sqlite` dialects. To try another one, add its blank import to `internal/testdb/testdb.go` (and keep it in your PR if the test passes).

A few tests are SQLite-only by design (they need two database files or SQLite pragmas), so they use `Dialect: "sqlite"` explicitly.

## Code guidelines

- **Format and vet:** `gofmt` and `go vet` clean. No other linter is required.
- **Errors:** return `*migro.Error` with a `Code` from `errors.go`, wrapping the cause with `migro.Wrap(code, err, "what failed")`. Add a new code only when callers need to tell the case apart, and document it in `docs/errors.md`.
- **Context first:** any function that does I/O takes `ctx context.Context` as its first parameter.
- **SQL safety:** values are always bound as parameters; identifiers go through `dialect.Quote`. Anything that accepts raw SQL (`Raw`, `DefaultRaw`, `Where` fragments, `Join` clauses) must be documented as trusted input, never built from user input.
- **Database-specific code stays in `dialect/<name>`.** If you need an `if dialect == "mysql"` elsewhere, add a method to the `Dialect` interface instead.
- **Dependencies:** keep them minimal. A new dependency needs a reason in the PR description.
- **No surprises:** destructive operations (dropping tables, columns, data) must be opt-in and named so.
- **Tests:** every behavior change comes with a test that fails without it. Prefer table-driven tests, use `internal/testdb`, and make sure they pass on SQLite. SQL that differs per database gets a golden-fragment test (see `TestCreateSQLPerDialect` in `schema/schema_test.go`).
- **Docs:** if behavior a user can see changes, update the matching page in `docs/` in the same PR.

## Common changes

### Add a database dialect

1. Create `dialect/<name>/<name>.go`. Embed `dialect.Base` and implement the rest of the `dialect.Dialect` interface: `Name`, `Dialector`, `DriverName`, `FromConn`, `Quote`, `ColumnType`, `AutoIncrement`, the rename and drop methods, and the `Supports*` / `TransactionalDDL` flags. Return `""` from a SQL method the database cannot do.
2. Register it in `init()` with `dialect.Register(&yourDialect{}, "alias", ...)`.
3. Add its name to `dialectPackages` in `cmd/migro/launcher.go` so the CLI can import it.
4. Add golden assertions for it to `TestCreateSQLPerDialect`.
5. If you can test it live, add its import to `internal/testdb/testdb.go` and say in the PR how you ran the suite.
6. Update the tables in `docs/installation.md`, `docs/databases.md` and the README.

### Add a column type or modifier

1. Add a `Kind` constant in `dialect/dialect.go` and map it in **every** dialect's `ColumnType`.
2. Add the method to `schema/table.go`, and the matching cases in `migrate/diff/diff.go` (`apply`) and `migrate/diff/gosource.go` (`colCode`).
3. Add golden tests and document it in `docs/schema-builder.md`.

### Add a CLI command

1. Add an entry to `allCommands()` in `console/commands.go` with its flags and usage string, and the handler.
2. Test it in `console/console_test.go` (the harness runs commands against SQLite and captures output).
3. Add it to the command table in `docs/command-line.md`.

## Commits and pull requests

1. **Branch from `main`** and keep the branch to one concern. Several small PRs are easier to review than one large one.
2. **Commit messages** follow Conventional Commits, as in the history: `feat: ...`, `fix: ...`, `test: ...`, `docs: ...`, `refactor: ...`, `chore: ...`. The first line says what changed; the body says why when it is not obvious.
3. **Do not mix unrelated changes**: no drive-by reformatting, renames or dependency bumps.
4. **Run the checks** from [Set up](#set-up) and, if your change touches SQL generation or the migrator, run the suite against PostgreSQL or MySQL too.
5. **Open the pull request against `main`** and fill in the template: what and why, how you tested it, and the checklist. Link the issue with `Fixes #123` when there is one.
6. **CI must be green.** All three jobs (SQLite, PostgreSQL and MySQL) are required: a failure on a live server usually points at a real portability bug.
7. **Review:** a maintainer will review. Push follow-up commits to the same branch rather than opening a new PR. PRs may be squash-merged, so write the PR title like a good commit message.

### Pull request checklist

- [ ] `gofmt -l .` prints nothing, `go vet ./...` and `go test -race ./...` pass
- [ ] New or changed behavior has tests, including the error path
- [ ] Docs in `docs/` (and the README if it is a headline feature) are updated
- [ ] No unrelated changes; new dependencies are justified
- [ ] Destructive behavior, if any, is opt-in and documented

## Documentation

The site you are reading is plain Markdown in `docs/`, built by GitHub Pages with the [just-the-docs](https://just-the-docs.com) theme. To change a page, edit its `.md` file.

- Every page starts with front matter (`title`, `nav_order`, `description`). `nav_order` sets the order in the sidebar.
- Link to other pages with relative links such as `[Migrations](migrations.md)`. They work on the site and when reading on GitHub.
- Examples must be real: run them before you commit them. Show output only when you captured it.
- Callouts: put `{: .note }` or `{: .warning }` on the line just before the paragraph (it shows as plain text when viewed on GitHub, which is fine).

To preview locally you need Ruby:

```
cd docs
bundle install
bundle exec jekyll serve
```

Then open http://localhost:4000/migro/. If you do not have Ruby, open the PR anyway; the Markdown is readable on GitHub.

## License

migro is MIT licensed. By contributing you agree that your contribution is released under the same license.
