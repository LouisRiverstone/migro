package orm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
	"gorm.io/gorm"
)

type Country struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}
type Citizen struct {
	ID        uint `gorm:"primaryKey"`
	CountryID uint
	Name      string
}
type Story struct {
	ID        uint `gorm:"primaryKey"`
	CitizenID uint
	Title     string
}
type Tag struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}
type Article struct {
	ID    uint `gorm:"primaryKey"`
	Title string
}
type Video struct {
	ID    uint `gorm:"primaryKey"`
	Title string
}
type Comment struct {
	ID              uint `gorm:"primaryKey"`
	CommentableType string
	CommentableID   uint
	Body            string
}

func relSetup(t *testing.T) *migro.DB {
	t.Helper()
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	for name, fn := range map[string]func(*schema.Table){
		"countries": func(t *schema.Table) { t.Increments("id"); t.String("name") },
		"citizens":  func(t *schema.Table) { t.Increments("id"); t.Integer("country_id"); t.String("name") },
		"stories":   func(t *schema.Table) { t.Increments("id"); t.Integer("citizen_id"); t.String("title") },
		"tags":      func(t *schema.Table) { t.Increments("id"); t.String("name") },
		"articles":  func(t *schema.Table) { t.Increments("id"); t.String("title") },
		"videos":    func(t *schema.Table) { t.Increments("id"); t.String("title") },
		"comments": func(t *schema.Table) {
			t.Increments("id")
			t.String("commentable_type")
			t.Integer("commentable_id")
			t.String("body")
		},
		"taggables": func(t *schema.Table) {
			t.Integer("tag_id")
			t.Integer("taggable_id")
			t.String("taggable_type")
		},
	} {
		if err := s.CreateTable(ctx, name, fn); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestHasManyThrough(t *testing.T) {
	ctx := context.Background()
	db := relSetup(t)
	g := db.Gorm
	g.Create(&[]Country{{Name: "BR"}, {Name: "PT"}, {Name: "NZ"}})
	g.Create(&[]Citizen{{CountryID: 1, Name: "ana"}, {CountryID: 1, Name: "bia"}, {CountryID: 2, Name: "caio"}})
	g.Create(&[]Story{{CitizenID: 1, Title: "s1"}, {CitizenID: 1, Title: "s2"}, {CitizenID: 2, Title: "s3"}, {CitizenID: 3, Title: "s4"}})

	rel := orm.HasManyThrough[Story, Citizen]{ForeignKey: "country_id", ThroughForeignKey: "citizen_id",
		Scope: func(q *gorm.DB) *gorm.DB { return q.Order("id ASC") }}
	got, err := rel.Load(ctx, db, []any{uint(1), uint(2), uint(3), uint(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got[uint(1)]) != 3 || len(got[uint(2)]) != 1 || len(got[uint(3)]) != 0 {
		t.Fatalf("got %+v", got)
	}
	if got[uint(1)][0].Title != "s1" || got[uint(2)][0].Title != "s4" {
		t.Fatalf("wrong rows: %+v", got)
	}
	// Parent keys given as another integer type still group correctly.
	got2, _ := rel.Load(ctx, db, []any{int64(2)})
	if len(got2[int64(2)]) != 1 {
		t.Fatalf("key type mismatch: %+v", got2)
	}
	if _, err := (orm.HasManyThrough[Story, Citizen]{}).Load(ctx, db, []any{1}); migro.CodeOf(err) != migro.InvalidSchema {
		t.Fatalf("missing keys should fail: %v", err)
	}
	if m, err := rel.Load(ctx, db, nil); err != nil || len(m) != 0 {
		t.Fatalf("empty keys: %v %v", m, err)
	}
}

func TestMorphTo(t *testing.T) {
	ctx := context.Background()
	db := relSetup(t)
	db.Gorm.Create(&[]Article{{Title: "a1"}, {Title: "a2"}})
	db.Gorm.Create(&[]Video{{Title: "v1"}})
	reg := orm.NewMorphRegistry()
	if err := orm.RegisterMorph[Article](reg, db, "article"); err != nil {
		t.Fatal(err)
	}
	if err := orm.RegisterMorph[Video](reg, db, "video"); err != nil {
		t.Fatal(err)
	}
	refs := []orm.Ref{{"article", uint(2)}, {"video", uint(1)}, {"article", uint(99)}}
	got, err := reg.Load(ctx, db, refs)
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := got[refs[0]].(*Article); !ok || a.Title != "a2" {
		t.Fatalf("article: %+v", got[refs[0]])
	}
	if v, ok := got[refs[1]].(*Video); !ok || v.Title != "v1" {
		t.Fatalf("video: %+v", got[refs[1]])
	}
	if _, ok := got[refs[2]]; ok {
		t.Fatal("missing row must be absent")
	}
	_, err = reg.Load(ctx, db, []orm.Ref{{"ghost", 1}})
	if migro.CodeOf(err) != migro.UnknownMorphType {
		t.Fatalf("want UNKNOWN_MORPH_TYPE, got %v", err)
	}
}

func TestMorphToManyAndInverse(t *testing.T) {
	ctx := context.Background()
	db := relSetup(t)
	db.Gorm.Create(&[]Tag{{Name: "go"}, {Name: "orm"}, {Name: "sql"}})
	db.Gorm.Create(&[]Article{{Title: "a1"}, {Title: "a2"}})
	db.Gorm.Create(&[]Video{{Title: "v1"}})
	pivot := func(typ string) orm.Pivot {
		return orm.Pivot{Table: "taggables", MorphIDColumn: "taggable_id", MorphTypeCol: "taggable_type", RelatedIDCol: "tag_id", MorphType: typ}
	}
	articleTags := orm.MorphToMany[Tag]{Pivot: pivot("article")}
	videoTags := orm.MorphToMany[Tag]{Pivot: pivot("video")}

	if err := articleTags.Attach(ctx, db, uint(1), uint(1), uint(2)); err != nil {
		t.Fatal(err)
	}
	if err := articleTags.Attach(ctx, db, uint(1), uint(2), uint(3)); err != nil { // 2 already linked
		t.Fatal(err)
	}
	if err := videoTags.Attach(ctx, db, uint(1), uint(1)); err != nil {
		t.Fatal(err)
	}
	var rows int64
	db.Gorm.Table("taggables").Count(&rows)
	if rows != 4 {
		t.Fatalf("attach must be idempotent, rows=%d", rows)
	}

	got, err := articleTags.Load(ctx, db, []any{uint(1), uint(2)})
	if err != nil || len(got[uint(1)]) != 3 || len(got[uint(2)]) != 0 {
		t.Fatalf("load: %+v %v", got, err)
	}

	// Inverse: which articles / videos carry tag 1?
	byTag, err := orm.MorphedByMany[Article]{Pivot: pivot("article")}.Load(ctx, db, []any{uint(1)})
	if err != nil || len(byTag[uint(1)]) != 1 || byTag[uint(1)][0].Title != "a1" {
		t.Fatalf("inverse articles: %+v %v", byTag, err)
	}
	vids, _ := orm.MorphedByMany[Video]{Pivot: pivot("video")}.Load(ctx, db, []any{uint(1)})
	if len(vids[uint(1)]) != 1 {
		t.Fatalf("inverse videos: %+v", vids)
	}

	if err := articleTags.Sync(ctx, db, uint(1), uint(3), uint(1)); err != nil {
		t.Fatal(err)
	}
	synced, _ := articleTags.Load(ctx, db, []any{uint(1)})
	names := map[string]bool{}
	for _, tg := range synced[uint(1)] {
		names[tg.Name] = true
	}
	if len(names) != 2 || !names["go"] || !names["sql"] {
		t.Fatalf("sync result: %v", names)
	}
	// Video links are untouched by article operations.
	vt, _ := videoTags.Load(ctx, db, []any{uint(1)})
	if len(vt[uint(1)]) != 1 {
		t.Fatal("sync leaked across morph types")
	}

	if err := articleTags.Detach(ctx, db, uint(1), uint(3)); err != nil {
		t.Fatal(err)
	}
	if err := articleTags.Detach(ctx, db, uint(1)); err != nil {
		t.Fatal(err)
	}
	left, _ := articleTags.Load(ctx, db, []any{uint(1)})
	if len(left[uint(1)]) != 0 {
		t.Fatalf("detach all failed: %+v", left)
	}
	if err := (orm.MorphToMany[Tag]{}).Attach(ctx, db, 1, 1); !errors.Is(err, migro.Newf(migro.InvalidSchema, "")) {
		t.Fatalf("empty pivot should fail: %v", err)
	}
}
