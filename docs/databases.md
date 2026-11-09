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
| PostgreSQL | `postgres` | Full suite in a CI job that does not block merges yet | yes |
| MySQL | `mysql` | Full suite in a CI job that does not block merges yet | no |
| MariaDB | `mariadb` | Generated SQL unit-tested; never run on a live server | no |
| CockroachDB | `cockroachdb` | Generated SQL unit-tested; never run on a live server | no (schema changes are not wrapped) |
| Redshift | `redshift` | Generated SQL unit-tested; never run on a live server | yes |
| SQL Server | `sqlserver` | Generated SQL unit-tested; never run on a live server | yes |
| Oracle | `oracle` | Generated SQL unit-tested; never run on a live server | no |

"Never run on a live server" means the SQL migro produces is checked against expected fragments, but nobody has executed it against the real database yet. Expect to find rough edges there, and please report them or [help verify](contributing.md#running-tests-against-a-real-database).

"Transactional DDL" decides whether a migration runs inside a transaction (see [Migrations](migrations.md#behavior-to-know)). Where it is `no`, a migration that fails halfway can leave partial changes, so keep migrations small.

## Per-database notes

- **MySQL / MariaDB:** `Increments`/`BigIncrements` are `unsigned`, so a foreign-key column must be `Integer(...).Unsigned()` / `BigInteger(...).Unsigned()` to match, otherwise MySQL rejects the constraint. `ForeignID` already does this. Enums are native.
- **SQLite:** cannot add a foreign key or primary key to an existing table (`UNSUPPORTED_FEATURE`); a unique constraint added with `AlterTable` becomes a unique index. Pure Go driver, no cgo.
- **Redshift:** no `CREATE INDEX` (`UNSUPPORTED_FEATURE`); JSON maps to `SUPER`; keys use `IDENTITY`. Constraints are informational only.
- **SQL Server:** `Enum` is `nvarchar` plus a `CHECK`; `Boolean` is `bit`.
- **Oracle:** identifiers are upper-cased; `Time` is stored as `VARCHAR2(15)` since Oracle has no `TIME` type.
- **CockroachDB:** uses the PostgreSQL driver; schema changes are not wrapped in transactions.

DSN formats are on [Connecting](connecting.md).
