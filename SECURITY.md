# Security policy

## Supported versions

migro is pre-1.0. Security fixes go into the latest release only.

## Reporting a vulnerability

Please do not open a public issue. Report it privately through GitHub: **Security → Report a vulnerability** on the [repository page](https://github.com/LouisRiverstone/migro/security/advisories/new).

Include the migro version, the database and dialect, a minimal reproduction, and what an attacker could do with it. You will get an answer within a few days, and a fix or a clear decision after that. Credit is given in the release notes unless you prefer otherwise.

## What counts

- SQL injection through an API that documents its argument as safe (column and table names passed to `Where*`, `OrderBy`, `GroupBy`, `WhereJSON` and the schema builder are quoted; `*Raw` methods and `Select` expressions are sent as written, by design).
- Mass-assignment or hidden-attribute leaks past `Fillable`, `Guarded` or `Hidden`.
- Weak defaults in the `Hashed` and `Encrypted` casts.
- A migration or CLI command that runs against the wrong database or skips the production confirmation.

Using `*Raw` methods with unsanitised user input is an application bug, not a migro vulnerability.
