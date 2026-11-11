package orm_test

import (
	"context"
	"testing"

	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
)

type pRole struct {
	orm.BaseModel
	Name string
}

func (pRole) TableName() string { return "p_roles" }

// pGrant is the pivot row: it has its own uuid key and a column with a default.
type pGrant struct {
	orm.UUIDKey
	PUserID uint   `gorm:"column:p_user_id"`
	PRoleID uint   `gorm:"column:p_role_id"`
	Level   string `gorm:"default:read"`
}

func (pGrant) TableName() string { return "p_grants" }

type pUser struct {
	orm.BaseModel
	Name  string
	Roles orm.BelongsToMany[pRole] `orm:"pivotTable:p_grants;pivotForeignKey:p_user_id;pivotRelatedForeignKey:p_role_id"`
}

func (pUser) TableName() string { return "p_users" }
func (pUser) PivotModels() map[string]any {
	return map[string]any{"Roles": pGrant{}}
}

func TestPivotModel(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	ok(t, s.CreateTable(ctx, "p_users", func(t *schema.Table) { t.Increments("id"); t.String("name"); t.Timestamps() }))
	ok(t, s.CreateTable(ctx, "p_roles", func(t *schema.Table) { t.Increments("id"); t.String("name"); t.Timestamps() }))
	ok(t, s.CreateTable(ctx, "p_grants", func(t *schema.Table) {
		t.String("id", 36).NotNull()
		t.Primary("id")
		t.Integer("p_user_id")
		t.Integer("p_role_id")
		t.String("level").Default("none")
	}))

	u := &pUser{Name: "ana"}
	ok(t, orm.New[pUser](db).Create(ctx, u))
	r := &pRole{Name: "admin"}
	ok(t, orm.New[pRole](db).Create(ctx, r))
	ok(t, u.Roles.Attach(ctx, r))

	var grants []pGrant
	ok(t, db.Gorm.Find(&grants).Error)
	if len(grants) != 1 || len(grants[0].ID) != 36 || grants[0].PUserID != u.ID || grants[0].PRoleID != r.ID {
		t.Fatalf("pivot row written through its model: %+v", grants)
	}
	if got := must(u.Roles.Get(ctx)); len(got) != 1 || got[0].Name != "admin" {
		t.Fatalf("read back: %+v", got)
	}
	ok(t, u.Roles.Detach(ctx, r))
	if got := must(u.Roles.Get(ctx)); len(got) != 0 {
		t.Fatalf("detach: %+v", got)
	}
}
