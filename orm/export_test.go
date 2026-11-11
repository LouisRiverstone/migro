package orm

import "reflect"

// ResetMorphMapForTest forgets every alias and the enforcement flag, so a test can exercise the
// global morph map without leaking into the others.
func ResetMorphMapForTest() {
	morphMap.Lock()
	defer morphMap.Unlock()
	morphMap.byAlias, morphMap.byType, morphMap.enforced = map[string]reflect.Type{}, map[reflect.Type]string{}, false
}
