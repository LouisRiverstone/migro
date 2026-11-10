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
