package orm

import (
	"reflect"
	"sync"
)

// typeCache memoizes a value derived from a reflect.Type.
type typeCache[V any] struct{ m sync.Map }

func newTypeCache[V any]() *typeCache[V] { return &typeCache[V]{} }

func (c *typeCache[V]) get(t reflect.Type, build func() V) V {
	if v, ok := c.m.Load(t); ok {
		return v.(V)
	}
	v := build()
	c.m.Store(t, v)
	return v
}
