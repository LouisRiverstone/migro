---
title: Database notes
nav_order: 18
description: What is verified on each database, and per-database behavior and limits.
---

# Database notes

## What is verified

| Database | Dialect name | Verified how | Transactional DDL |
|---|---|---|---|
| SQLite | `sqlite` | Full test suite in CI (blocking) | yes |
| PostgreSQL | `postgres` | Full suite in a CI job (blocking) | yes |
| MySQL | `mysql` | Full suite in a CI job (blocking) | no |
| MariaDB | `mariadb` | Full suite in a non-blocking CI job, results pending | no |
| CockroachDB | `cockroachdb` | Full suite in a non-blocking CI job, results pending | no (schema changes are not wrapped) |
| Redshift | `redshift` | Generated SQL unit-tested; never run on a live server | yes |
| SQL Server | `sqlserver` | Full suite in a non-blocking CI job, results pending | yes |
| Oracle | `oracle` | Generated SQL unit-tested; never run on a live server | no |

"Never run on a live server" means the SQL migro produces is checked against expected fragments, but nobody has executed it against the real database yet. Expect to find rough edges there, and please report them or [help verify](contributing.md#running-tests-against-a-real-database).

"Transactional DDL" decides whether a migration runs inside a transaction (see [Migrations](migrations.md#behavior-to-know)). Where it is `no`, a migration that fails halfway can leave partial changes, so keep migrations small.

## Feature support

Every dialect handles the schema builder, `With`/`WhereHas`, aggregates, pagination and casts. These are the places where it differs:

| Database | Row locks | JSON paths | Full-text | `Upsert` | `InsertOrIgnore` |
|---|---|---|---|---|---|
| PostgreSQL | update, share | yes | yes | `ON CONFLICT` | yes |
| CockroachDB | update, share | yes | yes | `ON CONFLICT` | yes |
| Redshift | no | yes | no | no | no |
| MySQL | update, share | yes | yes | `ON DUPLICATE KEY` | yes |
| MariaDB | update, share | yes | yes | `ON DUPLICATE KEY` | yes |
| SQLite | no (writes lock the file) | yes | no | `ON CONFLICT` | yes |
| SQL Server | no | yes | no | `MERGE` | no, use `Upsert` |
| Oracle | update only | yes | no | `MERGE` | no, use `Upsert` |

"No" means the call returns an `UNSUPPORTED_FEATURE` error instead of running something wrong. Check the code with `migro.CodeOf(err)`. Native enums exist on MySQL and MariaDB only; elsewhere `Enum` is a string with a `CHECK`.

## Per-database notes

- **MySQL / MariaDB:** `Increments`/`BigIncrements` are `unsigned`, so a foreign-key column must be `Integer(...).Unsigned()` / `BigInteger(...).Unsigned()` to match, otherwise MySQL rejects the constraint. `ForeignID` already does this. Enums are native. DDL is not transactional, so a migration that fails halfway leaves its earlier statements applied. An index on a column that already has a foreign key replaces the key's automatic index, and MySQL then refuses to drop it (error 1553) until the key is gone.
- **SQLite:** cannot add a foreign key or primary key to an existing table (`UNSUPPORTED_FEATURE`); a unique constraint added with `AlterTable` becomes a unique index. Pure Go driver, no cgo.
- **Redshift:** no `CREATE INDEX` (`UNSUPPORTED_FEATURE`); JSON maps to `SUPER`; keys use `IDENTITY`. Constraints are informational only.
- **SQL Server:** `Enum` is `nvarchar` plus a `CHECK`; `Boolean` is `bit`.
- **Oracle:** identifiers are upper-cased; `Time` is stored as `VARCHAR2(15)` since Oracle has no `TIME` type.
- **CockroachDB:** uses the PostgreSQL driver; schema changes are not wrapped in transactions.

DSN formats are on [Connecting](connecting.md).
