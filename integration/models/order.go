package models

import (
	"time"

	"github.com/LouisRiverstone/migro/orm"
)

type Order struct {
	orm.BaseModel
	UserID   uint
	Status   string
	Total    float64
	PlacedAt *time.Time

	User  orm.BelongsTo[User]
	Items orm.HasMany[OrderItem]
}

// OrderItem has a composite primary key (order_id, line_no), so it does not embed BaseModel;
// it still takes part in relations as the related side and as a belongs-to owner.
type OrderItem struct {
	OrderID   uint `gorm:"primaryKey"`
	LineNo    uint `gorm:"primaryKey;autoIncrement:false"`
	ProductID uint
	Quantity  int
	UnitPrice float64

	Order   orm.BelongsTo[Order]
	Product orm.BelongsTo[Product]
}
