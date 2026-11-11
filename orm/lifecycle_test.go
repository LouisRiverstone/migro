package orm_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/events"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
)

type userObserver struct {
	calls  []string
	refuse bool
}

func (o *userObserver) Creating(_ context.Context, u *eUser) error {
	o.calls = append(o.calls, "creating")
	if o.refuse {
		return errors.New("no more users")
	}
	return nil
}
func (o *userObserver) Created(_ context.Context, u *eUser) error {
	o.calls = append(o.calls, "created")
	return nil
}
func (o *userObserver) Retrieved(_ context.Context, u *eUser) error {
	o.calls = append(o.calls, "retrieved")
	return nil
}
func (o *userObserver) Deleted(_ context.Context, u *eUser) error {
	o.calls = append(o.calls, "deleted")
	return nil
}
func (o *userObserver) Restoring(_ context.Context, u *eUser) error {
	o.calls = append(o.calls, "restoring")
	return nil
}
func (o *userObserver) Restored(_ context.Context, u *eUser) error {
	o.calls = append(o.calls, "restored")
	return nil
}
func (o *userObserver) ForceDeleting(_ context.Context, u *eUser) error {
	o.calls = append(o.calls, "forceDeleting")
	return nil
}
func (o *userObserver) ForceDeleted(_ context.Context, u *eUser) error {
	o.calls = append(o.calls, "forceDeleted")
	return nil
}
func (o *userObserver) Replicating(_ context.Context, u *eUser) error {
	o.calls = append(o.calls, "replicating")
	return nil
}

func TestObserver(t *testing.T) {
	db, ctx := eSetup(t)
	bus := events.New()
	ok(t, bus.Attach(db))
	obs := &userObserver{}
	off, err := events.Observe[eUser](bus, obs)
	ok(t, err)

	users := orm.New[eUser](db)
	u, err := users.CreateFrom(ctx, map[string]any{"name": "ana", "email": "a@x.io"})
	ok(t, err)
	if !slices.Equal(obs.calls, []string{"creating", "created"}) {
		t.Fatalf("create: %v", obs.calls)
	}

	obs.calls = nil
	_, err = users.Find(ctx, u.ID)
	ok(t, err)
	if !slices.Equal(obs.calls, []string{"retrieved"}) {
		t.Fatalf("find: %v", obs.calls)
	}

	obs.calls = nil
	ok(t, u.Delete(ctx))
	ok(t, u.Restore(ctx))
	cp, err := orm.Replicate(ctx, u)
	ok(t, err)
	_ = cp
	ok(t, u.ForceDelete(ctx))
	want := []string{"deleted", "restoring", "restored", "replicating", "forceDeleting", "deleted", "forceDeleted"}
	if !slices.Equal(obs.calls, want) {
		t.Fatalf("lifecycle:\n got %v\nwant %v", obs.calls, want)
	}

	// quiet saves fire nothing
	obs.calls = nil
	q := orm.Bind(db, &eUser{Name: "quiet", Email: "q@x.io"})
	ok(t, q.SaveQuietly(ctx))
	if len(obs.calls) != 0 || !q.Exists() {
		t.Fatalf("SaveQuietly: %v exists=%v", obs.calls, q.Exists())
	}

	// a before hook aborts the write
	obs.refuse = true
	if _, err := users.CreateFrom(ctx, map[string]any{"name": "bob"}); migro.CodeOf(err) != migro.EventAborted {
		t.Fatalf("refused create: %v", err)
	}
	if n := must(users.Query().Count(ctx)); n != 1 {
		t.Fatalf("the refused row must not exist: %d", n)
	}

	off()
	obs.calls = nil
	obs.refuse = false
	_, err = users.CreateFrom(ctx, map[string]any{"name": "after-off"})
	ok(t, err)
	if len(obs.calls) != 0 {
		t.Fatalf("off must unsubscribe: %v", obs.calls)
	}

	if _, err := events.Observe[eUser](bus, &struct{}{}); migro.CodeOf(err) != migro.InvalidConfig {
		t.Fatalf("an observer with no hook is a mistake: %v", err)
	}
}

func TestChangesReplicateAndIs(t *testing.T) {
	db, ctx := eSeed(t)
	users := orm.New[eUser](db)

	n := must(users.CreateFrom(ctx, map[string]any{"name": "new", "email": "n@x.io"}))
	if !n.WasRecentlyCreated() || !n.WasChanged("name") {
		t.Fatalf("after insert: created=%v changes=%v", n.WasRecentlyCreated(), n.GetChanges())
	}
	ok(t, n.Save(ctx)) // nothing changed
	if n.WasChanged() || n.WasRecentlyCreated() {
		t.Fatalf("a no-op save changes nothing: %v", n.GetChanges())
	}
	n.Name = "renamed"
	ok(t, n.Save(ctx))
	if !n.WasChanged("name") || n.WasChanged("email") || n.GetChanges()["name"] != "renamed" {
		t.Fatalf("after update: %v", n.GetChanges())
	}

	ana := must(users.Query().With("Posts").Where("name = ?", "ana").First(ctx))
	cp := must(orm.Replicate(ctx, ana, "email"))
	if cp.ID != 0 || cp.Exists() || cp.Email != "" || cp.Name != "ana" || cp.Posts.Loaded() || !cp.CreatedAt.IsZero() {
		t.Fatalf("replica: %+v", cp)
	}
	ok(t, cp.Save(ctx))
	if cp.ID == 0 || cp.ID == ana.ID || !cp.Exists() {
		t.Fatalf("saved replica: %+v", cp)
	}
	if !orm.Is(ana, must(users.Find(ctx, ana.ID))) || orm.IsNot(ana, ana) || orm.Is(ana, cp) || orm.Is(ana, &eUser{}) || orm.Is(ana, &ePost{}) {
		t.Fatal("Is compares type and primary key")
	}
	if _, err := orm.Replicate(ctx, &eUser{}); migro.CodeOf(err) != migro.UnboundModel {
		t.Fatalf("unbound: %v", err)
	}
}

func TestDefaultsSerializeAsAndHelpers(t *testing.T) {
	db, ctx := fSetup(t)
	books := orm.New[fBook](db)
	if b := must(books.Make(map[string]any{"title": "x"})); b.Price != 99 {
		t.Fatalf("default: %d", b.Price)
	}
	if b := must(books.Make(map[string]any{"title": "x", "price": 5})); b.Price != 5 {
		t.Fatalf("attrs beat defaults: %d", b.Price)
	}

	tag := must(orm.New[fTag](db).Find(ctx, 1))
	m := must(orm.ToMap(tag))
	if m["label"] != "t1" || m["name"] != nil {
		t.Fatalf("SerializeAs: %v", m)
	}

	a := must(orm.New[fAuthor](db).Find(ctx, 1))
	ok(t, a.IncrementEach(ctx, map[string]int{"country_id": 5}))
	if a.CountryID != 6 {
		t.Fatalf("IncrementEach: %d", a.CountryID)
	}

	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	db.Gorm.Exec("UPDATE f_authors SET updated_at = ?", old)
	a = must(orm.New[fAuthor](db).Find(ctx, 1))
	a.Name = "quiet rename"
	ok(t, a.SaveWithoutTimestamps(ctx))
	fresh := must(orm.New[fAuthor](db).Find(ctx, 1))
	if fresh.Name != "quiet rename" || fresh.UpdatedAt.Year() != 2001 {
		t.Fatalf("SaveWithoutTimestamps: %+v", fresh)
	}
}

type kDoc struct {
	orm.UUIDKey
	Title string
}

func (kDoc) TableName() string { return "k_docs" }

type kNote struct {
	orm.ULIDKey
	Title string
}

func (kNote) TableName() string { return "k_notes" }

func TestUniqueKeys(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	ok(t, s.CreateTable(ctx, "k_docs", func(t *schema.Table) { t.String("id", 36).NotNull(); t.Primary("id"); t.String("title") }))
	ok(t, s.CreateTable(ctx, "k_notes", func(t *schema.Table) { t.String("id", 26).NotNull(); t.Primary("id"); t.String("title") }))

	d := &kDoc{Title: "a"}
	ok(t, orm.New[kDoc](db).Create(ctx, d))
	if len(d.ID) != 36 || d.ID[14] != '7' || d.ID[8] != '-' {
		t.Fatalf("uuid v7: %q", d.ID)
	}
	if got := must(orm.New[kDoc](db).Find(ctx, d.ID)); got.Title != "a" {
		t.Fatalf("find by uuid: %+v", got)
	}
	preset := &kDoc{UUIDKey: orm.UUIDKey{ID: "fixed"}, Title: "p"}
	ok(t, orm.New[kDoc](db).Create(ctx, preset))
	if preset.ID != "fixed" {
		t.Fatalf("an explicit id is kept: %q", preset.ID)
	}

	a, b := &kNote{Title: "a"}, &kNote{Title: "b"}
	ok(t, orm.New[kNote](db).Create(ctx, a))
	time.Sleep(3 * time.Millisecond)
	ok(t, orm.New[kNote](db).Create(ctx, b))
	if len(a.ID) != 26 || a.ID >= b.ID || strings.Trim(a.ID, "0123456789ABCDEFGHJKMNPQRSTVWXYZ") != "" {
		t.Fatalf("ulid: %q %q", a.ID, b.ID)
	}
	seen := map[string]bool{}
	for range 500 {
		id := orm.NewUUID()
		if seen[id] {
			t.Fatal("uuid collision")
		}
		seen[id] = true
	}
}

type uThing struct {
	orm.BaseModel
	SKU string
	Qty int
}

func (uThing) TableName() string { return "u_things" }

func TestUpsertInsertOrIgnoreFirstOrNew(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	ok(t, schema.New(db).CreateTable(ctx, "u_things", func(t *schema.Table) {
		t.Increments("id")
		t.String("sku").Unique()
		t.Integer("qty")
		t.Timestamps()
	}))
	things := orm.New[uThing](db)

	ok(t, things.Upsert(ctx, []uThing{{SKU: "a", Qty: 1}, {SKU: "b", Qty: 2}}, []string{"sku"}, []string{"qty"}))
	ok(t, things.Upsert(ctx, []uThing{{SKU: "a", Qty: 10}, {SKU: "c", Qty: 3}}, []string{"sku"}, []string{"qty"}))
	got := must(things.Query().OrderBy("sku", "asc").Get(ctx))
	if len(got) != 3 || got[0].Qty != 10 || got[1].Qty != 2 || got[2].Qty != 3 {
		t.Fatalf("Upsert: %+v", got)
	}
	ok(t, things.Upsert(ctx, []uThing{{SKU: "b", Qty: 20}}, []string{"sku"}, nil)) // empty update = all non-key columns
	if b := must(things.FindBy(ctx, "sku", "b")); b.Qty != 20 {
		t.Fatalf("Upsert with default columns: %+v", b)
	}
	if err := things.Upsert(ctx, []uThing{{SKU: "z"}}, nil, nil); migro.CodeOf(err) != migro.InvalidConfig {
		t.Fatalf("needs uniqueBy: %v", err)
	}

	ok(t, things.InsertOrIgnore(ctx, []uThing{{SKU: "a", Qty: 999}, {SKU: "d", Qty: 4}}))
	if a := must(things.FindBy(ctx, "sku", "a")); a.Qty != 10 {
		t.Fatalf("InsertOrIgnore must keep the existing row: %+v", a)
	}
	if n := must(things.Query().Count(ctx)); n != 4 {
		t.Fatalf("InsertOrIgnore: %d", n)
	}

	found, exists, err := things.FirstOrNew(ctx, uThing{SKU: "a"}, uThing{Qty: 7})
	ok(t, err)
	if !exists || found.Qty != 10 || !found.Exists() {
		t.Fatalf("FirstOrNew existing: %+v %v", found, exists)
	}
	fresh, exists, err := things.FirstOrNew(ctx, uThing{SKU: "new"}, uThing{Qty: 7})
	ok(t, err)
	if exists || fresh.Exists() || fresh.Qty != 7 || fresh.ID != 0 {
		t.Fatalf("FirstOrNew new: %+v %v", fresh, exists)
	}
	if n := must(things.Query().Count(ctx)); n != 4 {
		t.Fatalf("FirstOrNew must not insert: %d", n)
	}
}

type pSession struct {
	orm.BaseModel
	Name      string
	ExpiresAt time.Time
}

func (pSession) TableName() string { return "p_sessions" }
func (pSession) Prunable(q *orm.Query[pSession]) *orm.Query[pSession] {
	return q.Where("expires_at < ?", time.Now())
}

type pLog struct {
	orm.BaseModel
	Line string
	Old  bool
}

func (pLog) TableName() string { return "p_logs" }
func (pLog) MassPrunable(q *orm.Query[pLog]) *orm.Query[pLog] {
	return q.Where("old = ?", true)
}

func TestPrune(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	ok(t, s.CreateTable(ctx, "p_sessions", func(t *schema.Table) {
		t.Increments("id")
		t.String("name")
		t.Timestamp("expires_at")
		t.Timestamps()
	}))
	ok(t, s.CreateTable(ctx, "p_logs", func(t *schema.Table) {
		t.Increments("id")
		t.String("line")
		t.Boolean("old")
		t.Timestamps()
	}))
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	ok(t, orm.New[pSession](db).CreateMany(ctx, []pSession{{Name: "a", ExpiresAt: past}, {Name: "b", ExpiresAt: past}, {Name: "c", ExpiresAt: future}}))
	ok(t, orm.New[pLog](db).CreateMany(ctx, []pLog{{Line: "x", Old: true}, {Line: "y", Old: true}, {Line: "z"}}))

	bus := events.New()
	ok(t, bus.Attach(db))
	var deleted int
	events.OnModel(bus, events.AfterDelete, func(context.Context, *pSession) error { deleted++; return nil })
	events.OnModel(bus, events.AfterDelete, func(context.Context, *pLog) error { deleted += 100; return nil })

	if n, err := orm.New[pSession](db).Prune(ctx); err != nil || n != 2 || deleted != 2 {
		t.Fatalf("Prunable deletes one by one with events: n=%d deleted=%d err=%v", n, deleted, err)
	}
	if n, err := orm.New[pLog](db).Prune(ctx); err != nil || n != 2 || deleted != 2 {
		t.Fatalf("MassPrunable deletes in bulk without events: n=%d deleted=%d err=%v", n, deleted, err)
	}
	if n := must(orm.New[pSession](db).Query().Count(ctx)); n != 1 {
		t.Fatalf("live session kept: %d", n)
	}
	if _, err := orm.New[eUser](db).Prune(ctx); migro.CodeOf(err) != migro.InvalidConfig {
		t.Fatalf("not prunable: %v", err)
	}

	orm.RegisterPrunable[pSession]("sessions")
	orm.RegisterPrunable[pLog]("logs")
	got, err := orm.PruneAll(ctx, db)
	ok(t, err)
	if got["sessions"] != 0 || got["logs"] != 0 || len(got) < 2 {
		t.Fatalf("PruneAll: %v", got)
	}
}

type mAudit struct {
	orm.BaseModel
	Note string
}

func (mAudit) TableName() string  { return "m_audits" }
func (mAudit) Connection() string { return "audit" }

func TestUsingAndUseTransaction(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mgr, err := migro.NewManager("main", map[string]migro.Config{
		"main":  {Dialect: "sqlite", DSN: dir + "/main.db"},
		"audit": {Dialect: "sqlite", DSN: dir + "/audit.db"},
	})
	ok(t, err)
	t.Cleanup(func() { _ = mgr.Close() })
	audit := must(mgr.Connection(ctx, "audit"))
	ok(t, schema.New(audit).CreateTable(ctx, "m_audits", func(t *schema.Table) { t.Increments("id"); t.String("note"); t.Timestamps() }))

	logs := must(orm.Using[mAudit](ctx, mgr))
	ok(t, logs.Create(ctx, &mAudit{Note: "hello"}))
	if n := must(orm.New[mAudit](audit).Query().Count(ctx)); n != 1 {
		t.Fatalf("the model must live on the audit connection: %d", n)
	}
	main := must(mgr.Default(ctx))
	if has := must(schema.New(main).HasTable(ctx, "m_audits")); has {
		t.Fatal("main must not have the audit table")
	}

	// a model loaded outside a transaction joins one on demand
	row := must(logs.Find(ctx, 1))
	err = audit.Transaction(ctx, func(tx *migro.DB) error {
		row.UseTransaction(tx)
		row.Note = "inside"
		if err := row.Save(ctx); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil || must(logs.Find(ctx, 1)).Note != "hello" {
		t.Fatalf("the save must have rolled back with the transaction: %v", err)
	}
}
