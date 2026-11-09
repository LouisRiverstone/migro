package models

import "github.com/LouisRiverstone/migro/orm"

type Product struct {
	orm.BaseModel
	SKU   string `gorm:"column:sku"`
	Name  string
	Price float64
	Stock int
}
