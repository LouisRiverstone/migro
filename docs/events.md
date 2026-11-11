---
title: Events
nav_order: 15
description: Event bus and model lifecycle events.
---

# Events

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

migro events let you hook into the lifecycle of a model — creating, updating, deleting — and emit your own. They are the counterpart of Eloquent's model events and observers.

## Registering listeners

```go
bus := events.New()
if err := bus.Attach(db); err != nil { ... }

// Typed, per model. A before:* handler that returns an error aborts the write.
events.OnModel(bus, events.BeforeCreate, func(ctx context.Context, u *User) error {
    if u.Email == "" { return errors.New("email is required") }
    return nil
})
events.OnModel(bus, events.AfterCreate, func(ctx context.Context, u *User) error {
    return mailer.SendWelcome(u.Email)
})

// Free-form events and patterns
bus.On("after:*", logEvent)
bus.OnAny(audit)
off := bus.Once("user:welcomed", handler); off() // unsubscribe
err := bus.Emit(ctx, events.Event{Name: "user:welcomed", Payload: u})
bus.EmitAsync(ctx, ev, func(err error) { log.Println(err) }); bus.Wait()
```

## Available events

Names: `before:create`, `after:create`, `before:update`, `after:update`, `before:save`, `after:save` (create or update), `before:delete`, `after:delete`, `after:find`.

## Behavior

- `Emit` returns `EVENT_ABORTED` on the first `before:*` error and stops. For other events every handler runs and failures are combined into `EVENT_HANDLER_FAILED`. A handler panic becomes an error.
- Handlers run inside the operation's transaction, so an abort rolls the write back.
- Like GORM hooks, events do not fire for `UpdateColumn(s)`, raw SQL, `Exec`, or the query builder.
- Events are in-process only.

## Observers

An observer groups the hooks of one model in a type. It implements any of the hook interfaces from `events`, and the compiler checks each signature against the model:

```go
type UserObserver struct{ mailer Mailer }

func (o *UserObserver) Creating(ctx context.Context, u *User) error { return validate(u) }
func (o *UserObserver) Created(ctx context.Context, u *User) error  { return o.mailer.Welcome(u.Email) }

off, err := events.Observe[User](bus, &UserObserver{mailer})
```

| Hook | Event | Aborts on error |
|---|---|---|
| `Creating`, `Created` | `before:create`, `after:create` | `Creating` |
| `Updating`, `Updated` | `before:update`, `after:update` | `Updating` |
| `Saving`, `Saved` | `before:save`, `after:save` | `Saving` |
| `Deleting`, `Deleted` | `before:delete`, `after:delete` | `Deleting` |
| `Retrieved` | `after:find` | |
| `Restoring`, `Restored` | `before:restore`, `after:restore` | `Restoring` |
| `ForceDeleting`, `ForceDeleted` | `before:force_delete`, `after:force_delete` | `ForceDeleting` |
| `Replicating` | `before:replicate` | yes |

`Observe` returns an error when the type implements none of them, which usually means a misspelled method. `off()` removes every hook it added. `migro make:observer User` writes a starting file.

The restore, force-delete and replicate events come from the instance methods (`Restore`, `ForceDelete`, `orm.Replicate`), not from GORM, so they fire only for models bound to a database with an attached bus. A force delete also fires `before:delete` and `after:delete`.

Silence events for one call with `events.WithoutEvents(ctx)`, or use `SaveQuietly` and `DeleteQuietly`.
