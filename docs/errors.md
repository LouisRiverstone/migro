---
title: Errors
nav_order: 17
description: Stable error codes and how to match them.
---

# Errors

Every failure is a `*migro.Error` with a stable `Code`.

```go
user, err := users.Find(ctx, id)
switch {
case errors.Is(err, migro.ErrNotFound):
    // 404
case err != nil:
    var me *migro.Error
    if errors.As(err, &me) { log.Println(me.Code, me.Message) }
}
code := migro.CodeOf(err) // "" when err is not a migro error
```

`errors.Is` matches by code, so the sentinels `ErrNotFound`, `ErrMigrationLocked`, `ErrUnsupportedFeature`, `ErrPoolAcquireTimeout` and `ErrEventAborted` work through wrapped errors.

| Code | Meaning |
|---|---|
| `UNSUPPORTED_DIALECT` | Unknown dialect name, or its package was not imported |
| `CONNECTION_FAILED` | Could not connect, ping or close |
| `INVALID_CONFIG` | Bad pool, replica or cluster settings |
| `POOL_ACQUIRE_TIMEOUT` | No free connection within `AcquireTimeout` |
| `NOT_FOUND` | No matching row |
| `QUERY_FAILED` | The database rejected a statement |
| `INVALID_SCHEMA` | Bad table or column definition |
| `UNSUPPORTED_FEATURE` | The dialect cannot do this (for example `CREATE INDEX` on Redshift) |
| `MIGRATION_LOCKED` / `MIGRATION_FAILED` / `MIGRATION_CHECKSUM_MISMATCH` | Migration runner |
| `EVENT_ABORTED` / `EVENT_HANDLER_FAILED` | Event bus |
| `SEED_FAILED` | Seeder or factory failure |
| `PRODUCTION_BLOCKED` | A destructive command ran in production without `--force` or a confirmation |
| `DIFF_FAILED` | Schema diff failure |
| `SHARD_NOT_FOUND` / `SHARD_FAILED` | Cluster routing or fan-out |
| `UNKNOWN_MORPH_TYPE` | Polymorphic type not registered |
