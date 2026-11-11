package orm_test

import (
	"context"
	"testing"

	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
)

type uTeam struct {
	orm.SoftDeleteUUIDModel
	Name    string
	Members orm.HasMany[uMember]    `orm:"foreignKey:team_id"`
	Tags    orm.BelongsToMany[uTag] `orm:"pivotTable:u_team_tag;pivotForeignKey:team_id;pivotRelatedForeignKey:tag_id"`
}

func (uTeam) TableName() string { return "u_teams" }

type uMember struct {
	orm.ULIDModel
	TeamID string
	Name   string
	Team   orm.BelongsTo[uTeam] `orm:"foreignKey:team_id"`
}

func (uMember) TableName() string { return "u_members" }
func (uMember) Touches() []string { return []string{"Team"} }

type uTag struct {
	orm.UUIDModel
	Name string
}

func (uTag) TableName() string { return "u_tags" }

func TestStringKeyModels(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	ok(t, s.CreateTable(ctx, "u_teams", func(t *schema.Table) {
		t.String("id", 36).NotNull()
		t.Primary("id")
		t.String("name")
		t.Timestamps()
		t.SoftDeletes()
	}))
	ok(t, s.CreateTable(ctx, "u_members", func(t *schema.Table) {
		t.String("id", 26).NotNull()
		t.Primary("id")
		t.String("team_id", 36)
		t.String("name")
		t.Timestamps()
	}))
	ok(t, s.CreateTable(ctx, "u_tags", func(t *schema.Table) {
		t.String("id", 36).NotNull()
		t.Primary("id")
		t.String("name")
		t.Timestamps()
	}))
	ok(t, s.CreateTable(ctx, "u_team_tag", func(t *schema.Table) { t.String("team_id", 36); t.String("tag_id", 36) }))

	teams := orm.New[uTeam](db)
	team := &uTeam{Name: "core"}
	ok(t, teams.Create(ctx, team))
	if len(team.ID) != 36 || team.CreatedAt.IsZero() {
		t.Fatalf("create: %+v", team)
	}

	// instance methods work with a string key
	team = must(teams.Find(ctx, team.ID))
	team.Name = "platform"
	if !team.IsDirty() {
		t.Fatal("dirty tracking")
	}
	ok(t, team.Save(ctx))
	ok(t, team.Refresh(ctx))
	if team.Name != "platform" {
		t.Fatalf("save/refresh: %q", team.Name)
	}
	ok(t, team.Touch(ctx))

	m := &uMember{TeamID: team.ID, Name: "ana"}
	ok(t, orm.New[uMember](db).Create(ctx, m))
	if len(m.ID) != 26 {
		t.Fatalf("ulid: %q", m.ID)
	}
	members := must(team.Members.Get(ctx))
	if len(members) != 1 || members[0].Name != "ana" {
		t.Fatalf("has many: %+v", members)
	}
	owner := must(members[0].Team.Get(ctx))
	if owner == nil || owner.ID != team.ID {
		t.Fatalf("belongs to: %+v", owner)
	}
	ok(t, members[0].Save(ctx)) // touches the team

	tag := &uTag{Name: "go"}
	ok(t, orm.New[uTag](db).Create(ctx, tag))
	ok(t, team.Tags.Attach(ctx, tag))
	if got := must(team.Tags.Get(ctx)); len(got) != 1 || got[0].ID != tag.ID {
		t.Fatalf("many to many: %+v", got)
	}

	// soft delete + restore on a string key
	ok(t, team.Delete(ctx))
	if _, err := teams.Find(ctx, team.ID); err == nil {
		t.Fatal("soft deleted team should be hidden")
	}
	ok(t, team.Restore(ctx))
	if got := must(teams.Find(ctx, team.ID)); got.Name != "platform" {
		t.Fatalf("restore: %+v", got)
	}
	n, err := teams.Destroy(ctx, team.ID)
	ok(t, err)
	if n != 1 {
		t.Fatalf("destroy: %d", n)
	}
}

type uBare struct {
	orm.Record
	orm.UUIDKey
	Title string
}

func (uBare) TableName() string { return "u_bares" }

func TestRecordWithBareKey(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	ok(t, schema.New(db).CreateTable(ctx, "u_bares", func(t *schema.Table) { t.String("id", 36).NotNull(); t.Primary("id"); t.String("title") }))
	b := &uBare{Title: "a"}
	ok(t, orm.New[uBare](db).Create(ctx, b))
	b = must(orm.New[uBare](db).Find(ctx, b.ID))
	b.Title = "b"
	ok(t, b.Save(ctx))
	ok(t, b.Refresh(ctx))
	if b.Title != "b" {
		t.Fatalf("%+v", b)
	}
}
