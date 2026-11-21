---
title: Events
nav_order: 15
description: "Model lifecycle events, closures and observers, events and transactions, muting events, and the event bus for your own events."
---

# Events

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Models fire events at each moment of their life, so you can hook into it: validate before a create, send a welcome mail after it, clean up files after a delete. They are the counterpart of Eloquent's model events and observers, and they run on an **event bus** that you attach to a database. The same bus carries events of your own.

```go
bus := events.New()
if err := bus.Attach(db); err != nil {
    return err
}
```

Attach the bus once, at startup. Transactions and `WriteDB()` handles of `db` share it. `events.From(db)` returns the bus attached to a database.

### Available events

| Eloquent | migro event | Constant | Fires |
|---|---|---|---|
| `retrieved` | `after:find` | `events.AfterFind` | after a model is loaded |
| `creating`, `created` | `before:create`, `after:create` | `events.BeforeCreate`, `events.AfterCreate` | around an insert |
| `updating`, `updated` | `before:update`, `after:update` | `events.BeforeUpdate`, `events.AfterUpdate` | around an update |
| `saving`, `saved` | `before:save`, `after:save` | `events.BeforeSave`, `events.AfterSave` | around an insert or an update |
| `deleting`, `deleted` | `before:delete`, `after:delete` | `events.BeforeDelete`, `events.AfterDelete` | around a delete (soft or not) |
| `restoring`, `restored` | `before:restore`, `after:restore` | | around `Restore` |
| `forceDeleting`, `forceDeleted` | `before:force_delete`, `after:force_delete` | | around `ForceDelete` |
| `replicating` | `before:replicate` | | in `orm.Replicate` |
| `trashed` | `after:delete` on a soft-deleting model | | |

`before:*` events fire before the change reaches the database, `after:*` events after it. `saving` and `saved` fire for both inserts and updates.

The restore, force-delete and replicate events come from the instance methods (`Restore`, `ForceDelete`, `orm.Replicate`), so they fire only for bound models. A force delete also fires `before:delete` and `after:delete`.

{: .warning }
Events fire for models, not for queries. A mass update or delete through `Query()`, `Destroy`, the [query builder](query-builder.md) or raw SQL loads no model, so **no model event fires** for it. The same holds for GORM's `UpdateColumn(s)` and `Exec`.

Laravel's `$dispatchesEvents`, which maps a model event to an event class, is a listener that emits your own event (see [the event bus](#the-event-bus)).

## Using closures

`events.OnModel` registers a typed function for one model and one event. The compiler checks the model type:

```go
events.OnModel(bus, events.AfterCreate, func(ctx context.Context, u *User) error {
    return mailer.SendWelcome(ctx, u.Email)
})
```

A `before:*` handler that returns an error **aborts** the operation: the write does not happen and the call returns `EVENT_ABORTED`:

```go
events.OnModel(bus, events.BeforeCreate, func(ctx context.Context, u *User) error {
    if u.Email == "" {
        return errors.New("email is required")
    }
    return nil
})

err := orm.New[User](db).Create(ctx, &User{})
// migro.CodeOf(err) == migro.EventAborted
```

`OnModel` returns a function that removes the handler. The pattern can be a wildcard, such as `before:*`.

Laravel's `queueable()` closure runs the listener on a queue. To do the same, dispatch a job from the listener (with [perago](https://github.com/LouisRiverstone/perago), for instance), or use `bus.EmitAsync` for your own events.

## Observers

### Defining observers

An observer groups the handlers of one model in a type. It implements any of the hook methods; each one is checked against the model at compile time:

```go
type UserObserver struct{ mailer Mailer }

func (o *UserObserver) Creating(ctx context.Context, u *User) error { return validate(u) }
func (o *UserObserver) Created(ctx context.Context, u *User) error  { return o.mailer.Welcome(ctx, u.Email) }
func (o *UserObserver) Deleted(ctx context.Context, u *User) error  { return avatars.Remove(ctx, u.ID) }
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

```
migro make:observer User --model=example.com/app/models.User
```

writes `app/observers/user_observer.go` (`MIGRO_OBSERVERS_DIR`) with every hook stubbed.

#### Registering observers

```go
off, err := events.Observe[User](bus, &UserObserver{mailer: mailer})
```

`Observe` returns an error when the type implements none of the hooks, which usually means a misspelled method. `off()` removes every hook it added. Because you build the observer, it can carry its dependencies as fields.

### Observers and database transactions

Handlers run inside the operation's transaction. A `before:*` abort therefore rolls back the write, and an `after:*` error is returned from the call that runs in the transaction, which rolls it back.

Laravel's `ShouldHandleEventsAfterCommit` defers handlers until the commit. migro has no such switch: when a handler has side effects outside the database (mail, queues), dispatch them after `db.Transaction` returns, or use a queue that supports after-commit dispatch (perago's `peragomigro.Transaction`).

### Errors in handlers

`Emit` returns `EVENT_ABORTED` on the first `before:*` error and stops there. For the other events, every handler runs and their failures are combined into one `EVENT_HANDLER_FAILED`. A panic in a handler becomes an error.

## Muting events

`events.WithoutEvents(ctx)` returns a context in which no model event fires (Eloquent's `withoutEvents`):

```go
ctx := events.WithoutEvents(ctx)
user, err := orm.New[User](db).Find(ctx, 1)
err = user.Delete(ctx)
```

`events.Silenced(ctx)` reports whether a context is muted. Seeders can mute events for the whole run with `seed.WithoutEvents()`.

### Saving a single model without events

```go
err := user.SaveQuietly(ctx)
err = user.DeleteQuietly(ctx)
err = user.SaveWithoutTimestamps(ctx) // also leaves updated_at alone
```

## The event bus

The bus carries any event you name, not only model events:

```go
bus.On("user:welcomed", func(ctx context.Context, e events.Event) error {
    log.Printf("welcomed %v", e.Payload)
    return nil
})
bus.On("after:*", logEvent)   // a pattern
bus.OnAny(audit)              // every event
off := bus.Once("report:ready", handler)
off()                         // unsubscribe

err := bus.Emit(ctx, events.Event{Name: "user:welcomed", Payload: u})
bus.EmitAsync(ctx, events.Event{Name: "report:ready"}, func(err error) { log.Println(err) })
bus.Wait() // wait for the async handlers, at shutdown
```

An `events.Event` has a `Name`, the `Table` and `Model` for lifecycle events, and a free-form `Payload`. `events.EmitModel(ctx, db, name, table, model)` fires a model event yourself.

Events are in-process only. To reach other processes, publish to a queue or a message broker from a handler.
