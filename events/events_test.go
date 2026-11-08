package events_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/events"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
)

func TestBusMatchingOrderOnceOff(t *testing.T) {
	ctx := context.Background()
	b := events.New()
	var got []string
	rec := func(tag string) events.Handler {
		return func(_ context.Context, e events.Event) error { got = append(got, tag+":"+e.Name); return nil }
	}
	b.On("user:created", rec("exact"))
	b.On("user:*", rec("glob"))
	b.On("*:created", rec("suffix"))
	b.OnAny(rec("any"))
	b.Once("user:created", rec("once"))
	off := b.On("user:created", rec("off"))
	off()

	_ = b.Emit(ctx, events.Event{Name: "user:created"})
	_ = b.Emit(ctx, events.Event{Name: "user:created"})
	want := []string{"exact:user:created", "glob:user:created", "suffix:user:created", "any:user:created", "once:user:created",
		"exact:user:created", "glob:user:created", "suffix:user:created", "any:user:created"}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestBeforeAbortsAfterCollects(t *testing.T) {
	ctx := context.Background()
	b := events.New()
	ran := false
	b.On("before:x", func(context.Context, events.Event) error { return errors.New("no") })
	b.On("before:x", func(context.Context, events.Event) error { ran = true; return nil })
	err := b.Emit(ctx, events.Event{Name: "before:x"})
	if !errors.Is(err, migro.ErrEventAborted) || ran {
		t.Fatalf("before must abort and stop dispatch: err=%v ran=%v", err, ran)
	}

	second := false
	b.On("after:x", func(context.Context, events.Event) error { return errors.New("a") })
	b.On("after:x", func(context.Context, events.Event) error { second = true; return errors.New("b") })
	err = b.Emit(ctx, events.Event{Name: "after:x"})
	if migro.CodeOf(err) != migro.EventHandlerFailed || !second {
		t.Fatalf("after handlers must all run: err=%v second=%v", err, second)
	}
}

func TestPanicBecomesError(t *testing.T) {
	b := events.New()
	b.On("boom", func(context.Context, events.Event) error { panic("kaboom") })
	if err := b.Emit(context.Background(), events.Event{Name: "boom"}); err == nil {
		t.Fatal("panic should surface as error")
	}
}

func TestEmitAsyncWait(t *testing.T) {
	b := events.New()
	var mu sync.Mutex
	n := 0
	b.On("tick", func(context.Context, events.Event) error { mu.Lock(); n++; mu.Unlock(); return nil })
	for range 20 {
		b.EmitAsync(context.Background(), events.Event{Name: "tick"}, nil)
	}
	b.Wait()
	if n != 20 {
		t.Fatalf("n=%d", n)
	}
}

type Article struct {
	ID    uint `gorm:"primaryKey"`
	Title string
}

func TestModelLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	if err := schema.New(db).CreateTable(ctx, "articles", func(t *schema.Table) { t.Increments("id"); t.String("title") }); err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	if err := bus.Attach(db); err != nil {
		t.Fatal(err)
	}
	var log []string
	bus.OnAny(func(_ context.Context, e events.Event) error { log = append(log, e.Name); return nil })
	events.OnModel(bus, events.BeforeCreate, func(_ context.Context, a *Article) error {
		if a.Title == "forbidden" {
			return errors.New("title not allowed")
		}
		return nil
	})

	articles := orm.New[Article](db)
	a := &Article{Title: "ok"}
	if err := articles.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.Title = "changed"
	if err := articles.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := articles.Find(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := articles.Delete(ctx, a); err != nil {
		t.Fatal(err)
	}
	want := []string{"before:save", "before:create", "after:create", "after:save",
		"before:save", "before:update", "after:update", "after:save", "after:find", "before:delete", "after:delete"}
	if !slices.Equal(log, want) {
		t.Fatalf("got  %v\nwant %v", log, want)
	}

	// An aborting before-handler prevents the write and rolls the error back to the caller.
	err := articles.Create(ctx, &Article{Title: "forbidden"})
	if !errors.Is(err, migro.ErrEventAborted) {
		t.Fatalf("want EVENT_ABORTED, got %v", err)
	}
	if n, _ := articles.Query().Where("title = ?", "forbidden").Count(ctx); n != 0 {
		t.Fatal("aborted create must not persist")
	}
}

func TestMigrationTablesDoNotEmit(t *testing.T) {
	// Internal migro_* tables must never leak events (guards the plugin's table filter).
	ctx := context.Background()
	db := testdb.Open(t)
	bus := events.New()
	_ = bus.Attach(db)
	called := false
	bus.OnAny(func(context.Context, events.Event) error { called = true; return nil })
	_ = schema.New(db).CreateTable(ctx, "migro_seeds_probe", func(t *schema.Table) { t.Increments("id") })
	if called {
		t.Fatal("DDL must not emit model events")
	}
}
