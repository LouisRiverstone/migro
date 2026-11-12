---
title: Installation
nav_order: 2
description: Requirements, installing the library and the migro command, picking a database driver.
---

# Installation

## Requirements

- **Go 1.25 or newer.** The database drivers migro builds on require it.
- A supported database (or just SQLite to try things out, which needs nothing installed).
- For the `migro` command: the Go toolchain must be on `PATH` wherever you run it, because commands that touch the database compile a small runner inside your project (see [How it works](how-it-works.md#what-migro-does-when-you-run-a-command)). In production you can embed the same commands in your own binary and skip that.

## Install the library

Inside your Go module:

```
go get github.com/LouisRiverstone/migro@latest
```

`@latest` resolves to the newest tagged release. To pin one, use `go get github.com/LouisRiverstone/migro@v0.1.0`; to try unreleased work, use `@main` or a commit (`@<commit-sha>`). Releases are listed on [GitHub](https://github.com/LouisRiverstone/migro/releases).

## Pick a database driver

Each database is a separate package. Import the ones you use for their side effect; unused drivers are never compiled in.

| Database | Dialect name(s) | Import |
|---|---|---|
| PostgreSQL, CockroachDB, Redshift | `postgres`, `cockroachdb`, `redshift` | `_ "github.com/LouisRiverstone/migro/dialect/postgres"` |
| MySQL, MariaDB | `mysql`, `mariadb` | `_ "github.com/LouisRiverstone/migro/dialect/mysql"` |
| SQLite (pure Go, no cgo) | `sqlite` | `_ "github.com/LouisRiverstone/migro/dialect/sqlite"` |
| SQL Server | `sqlserver` | `_ "github.com/LouisRiverstone/migro/dialect/sqlserver"` |
| Oracle (pure Go, no cgo) | `oracle` | `_ "github.com/LouisRiverstone/migro/dialect/oracle"` |

If you forget the import, `migro.Open` returns an `UNSUPPORTED_DIALECT` error that names the package to import.

## Install the command line tool

```
go install github.com/LouisRiverstone/migro/cmd/migro@latest
```

This puts a `migro` binary in `$(go env GOPATH)/bin`. Make sure that folder is on your `PATH`, then check it:

```
migro help
```

You should see the list of `make:*`, `migration:*` and `db:seed` commands.

## Check that it works

Create a file anywhere in your module and run it:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/LouisRiverstone/migro"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
)

func main() {
	db, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: "check.db"})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	fmt.Println("connected:", db.Ping(context.Background()) == nil)
}
```

It should print `connected: true`. Delete `check.db` afterwards.

## Next

- [Getting started](getting-started.md) builds a small project step by step.
- [Connecting](connecting.md) lists the DSN format for every database.
