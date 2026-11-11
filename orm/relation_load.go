package orm

import (
	"cmp"
	"context"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

type scopeFn = func(*gorm.DB) *gorm.DB

func blank(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func asInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case uint64:
		return int64(x)
	case float64:
		return int64(x)
	case []byte:
		n, _ := strconv.ParseInt(string(x), 10, 64)
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}

func keysOf(ctx context.Context, d *relDef, owners []reflect.Value, col string) ([]any, error) {
	var raw []any
	for _, o := range owners {
		v, err := fieldOf(ctx, d.ownerSch, o, col)
		if err != nil {
			return nil, err
		}
		if !blank(v) {
			raw = append(raw, v)
		}
	}
	uniq, _ := uniqueKeys(raw)
	return uniq, nil
}

func relQuery(ctx context.Context, db *migro.DB, t reflect.Type, scope scopeFn) *gorm.DB {
	g := db.Gorm.WithContext(ctx).Model(reflect.New(t).Interface())
	if scopes := globalScopesOf(t); len(scopes) > 0 {
		g = g.Scopes(scopeFunc(scopes, &skipSet{}))
	}
	if scope != nil {
		g = scope(g)
	}
	return g
}

func findRows(g *gorm.DB, t reflect.Type, op string) (reflect.Value, error) {
	ptr := reflect.New(reflect.SliceOf(t))
	if err := g.Find(ptr.Interface()).Error; err != nil {
		return reflect.Value{}, wrap(err, op)
	}
	return ptr.Elem(), nil
}

func pick(t reflect.Type, rows reflect.Value, idx []int) reflect.Value {
	sl := reflect.MakeSlice(reflect.SliceOf(t), 0, len(idx))
	for _, i := range idx {
		sl = reflect.Append(sl, rows.Index(i))
	}
	return sl
}

// assign stores rows in owner's relation field and binds the freshly stored children.
func assign(db *migro.DB, owner reflect.Value, name string, rows reflect.Value) relField {
	rf := owner.FieldByName(name).Addr().Interface().(relField)
	rf.store(rows)
	for _, c := range rf.children() {
		bind(db, c.Interface(), true)
	}
	return rf
}

// loadPath eager-loads a (possibly dotted) relation path for owners. scope constrains the last segment.
func loadPath(ctx context.Context, db *migro.DB, owners []reflect.Value, path string, scope scopeFn) error {
	return loadPathOnce(ctx, db, owners, path, scope, "", map[string]bool{})
}

// loadPathOnce skips segments already loaded in this batch, so With("Posts.Comments").With("Posts.Tags")
// loads Posts once and keeps what the first path hung under it.
func loadPathOnce(ctx context.Context, db *migro.DB, owners []reflect.Value, path string, scope scopeFn, prefix string, done map[string]bool) error {
	byType := map[reflect.Type][]reflect.Value{}
	var order []reflect.Type
	for _, o := range owners {
		if _, ok := byType[o.Type()]; !ok {
			order = append(order, o.Type())
		}
		byType[o.Type()] = append(byType[o.Type()], o)
	}
	name, rest, nested := strings.Cut(path, ".")
	for _, t := range order {
		group := byType[t]
		d, _, err := resolveByName(db, t, name)
		if err != nil {
			return err
		}
		sc := scope
		if nested {
			sc = nil
		}
		key := prefix + t.String() + "." + name
		if !done[key] {
			if err := d.eager(ctx, db, group, sc); err != nil {
				return err
			}
			done[key] = true
		}
		if !nested {
			continue
		}
		var kids []reflect.Value
		for _, o := range group {
			for _, c := range o.FieldByName(name).Addr().Interface().(relField).children() {
				kids = append(kids, c.Elem())
			}
		}
		if err := loadPathOnce(ctx, db, kids, rest, scope, key+">", done); err != nil {
			return err
		}
	}
	return nil
}

// Load eager-loads relations onto an already fetched model (Eloquent's $model->load()).
func Load[T any](ctx context.Context, db *migro.DB, v *T, relations ...string) error {
	if v == nil {
		return nil
	}
	bindRelations(db, reflect.ValueOf(v))
	done := map[string]bool{}
	for _, r := range relations {
		if err := loadPathOnce(ctx, db, []reflect.Value{reflect.ValueOf(v).Elem()}, r, nil, "", done); err != nil {
			return err
		}
	}
	return nil
}

func (d *relDef) eager(ctx context.Context, db *migro.DB, owners []reflect.Value, scope scopeFn) error {
	in := func(col string) string { return inClause(db, col) }
	q := db.Dialect.Quote
	switch d.kind {
	case kHasOne, kHasMany, kMorphOne, kMorphMany:
		keys, err := keysOf(ctx, d, owners, d.lk)
		if err != nil {
			return err
		}
		rows := reflect.Value{}
		if len(keys) > 0 {
			g := relQuery(ctx, db, d.relT, scope).Where(in(d.fk), keys)
			if d.typeCol != "" {
				g = g.Where(q(d.typeCol)+" = ?", d.typeVal)
			}
			if d.ofMany != nil {
				cond, args := d.ofMany.cond(db, d)
				g = g.Where(cond, args...)
			}
			if rows, err = findRows(g, d.relT, "load "+d.name); err != nil {
				return err
			}
		}
		groups := map[string][]int{}
		for i := 0; rows.IsValid() && i < rows.Len(); i++ {
			v, err := fieldOf(ctx, d.relSch, rows.Index(i), d.fk)
			if err != nil {
				return err
			}
			groups[normKey(v)] = append(groups[normKey(v)], i)
		}
		for _, o := range owners {
			k, _ := fieldOf(ctx, d.ownerSch, o, d.lk)
			assign(db, o, d.name, pick(d.relT, rows, groups[normKey(k)]))
		}
	case kBelongsTo:
		keys, err := keysOf(ctx, d, owners, d.fk)
		if err != nil {
			return err
		}
		rows := reflect.Value{}
		if len(keys) > 0 {
			if rows, err = findRows(relQuery(ctx, db, d.relT, scope).Where(in(d.lk), keys), d.relT, "load "+d.name); err != nil {
				return err
			}
		}
		idx := map[string]int{}
		for i := 0; rows.IsValid() && i < rows.Len(); i++ {
			v, _ := fieldOf(ctx, d.relSch, rows.Index(i), d.lk)
			idx[normKey(v)] = i
		}
		for _, o := range owners {
			k, _ := fieldOf(ctx, d.ownerSch, o, d.fk)
			var pos []int
			if i, ok := idx[normKey(k)]; ok && !blank(k) {
				pos = []int{i}
			}
			assign(db, o, d.name, pick(d.relT, rows, pos))
		}
	case kBelongsToMany, kMorphToMany, kMorphedByMany:
		return d.eagerPivot(ctx, db, owners, scope)
	case kThrough:
		return d.eagerThrough(ctx, db, owners, scope)
	case kMorphTo:
		return d.eagerMorphTo(ctx, db, owners, scope)
	}
	return nil
}

func (p pivotDef) rows(ctx context.Context, db *migro.DB, keys []any) ([]map[string]any, error) {
	cols := append([]string{p.ownerCol, p.relatedCol}, p.extra...)
	g := db.Gorm.WithContext(ctx).Table(p.table).Select(cols).Where(inClause(db, p.ownerCol), keys)
	if p.typeCol != "" {
		g = g.Where(db.Dialect.Quote(p.typeCol)+" = ?", p.typeVal)
	}
	g = p.constrain(db, g)
	var out []map[string]any
	if err := g.Find(&out).Error; err != nil {
		return nil, wrap(err, "load pivot "+p.table)
	}
	return out, nil
}

func (d *relDef) eagerPivot(ctx context.Context, db *migro.DB, owners []reflect.Value, scope scopeFn) error {
	p := d.pivot
	keys, err := keysOf(ctx, d, owners, d.lk)
	if err != nil {
		return err
	}
	var prows []map[string]any
	rows := reflect.Value{}
	if len(keys) > 0 {
		if prows, err = p.rows(ctx, db, keys); err != nil {
			return err
		}
		var relKeys []any
		for _, r := range prows {
			relKeys = append(relKeys, r[p.relatedCol])
		}
		relKeys, _ = uniqueKeys(relKeys)
		if len(relKeys) > 0 {
			if rows, err = findRows(relQuery(ctx, db, d.relT, scope).Where(inClause(db, d.rk), relKeys), d.relT, "load "+d.name); err != nil {
				return err
			}
		}
	}
	idx := map[string]int{}
	for i := 0; rows.IsValid() && i < rows.Len(); i++ {
		v, _ := fieldOf(ctx, d.relSch, rows.Index(i), d.rk)
		idx[normKey(v)] = i
	}
	for _, o := range owners {
		k, _ := fieldOf(ctx, d.ownerSch, o, d.lk)
		var pos []int
		var links []map[string]any
		for _, pr := range prows {
			if normKey(pr[p.ownerCol]) != normKey(k) {
				continue
			}
			if i, ok := idx[normKey(pr[p.relatedCol])]; ok {
				pos, links = append(pos, i), append(links, pr)
			}
		}
		rf := assign(db, o, d.name, pick(d.relT, rows, pos))
		for i, c := range rf.children() {
			for col, val := range links[i] {
				setExtra(c.Interface(), p.prefix()+"_"+col, val)
			}
		}
	}
	return nil
}

func (d *relDef) eagerThrough(ctx context.Context, db *migro.DB, owners []reflect.Value, scope scopeFn) error {
	keys, err := keysOf(ctx, d, owners, d.lk)
	if err != nil {
		return err
	}
	toOwner := map[string]string{}
	rows := reflect.Value{}
	if len(keys) > 0 {
		var th []map[string]any
		g := db.Gorm.WithContext(ctx).Model(reflect.New(d.thSch.ModelType).Interface()).Select([]string{d.thKey, d.thFK}).Where(inClause(db, d.thFK), keys)
		if err := g.Find(&th).Error; err != nil {
			return wrap(err, "load through "+d.name)
		}
		var thKeys []any
		for _, r := range th {
			toOwner[normKey(r[d.thKey])] = normKey(r[d.thFK])
			thKeys = append(thKeys, r[d.thKey])
		}
		if thKeys, _ = uniqueKeys(thKeys); len(thKeys) > 0 {
			if rows, err = findRows(relQuery(ctx, db, d.relT, scope).Where(inClause(db, d.relFK), thKeys), d.relT, "load "+d.name); err != nil {
				return err
			}
		}
	}
	groups := map[string][]int{}
	for i := 0; rows.IsValid() && i < rows.Len(); i++ {
		v, _ := fieldOf(ctx, d.relSch, rows.Index(i), d.relFK)
		owner := toOwner[normKey(v)]
		groups[owner] = append(groups[owner], i)
	}
	for _, o := range owners {
		k, _ := fieldOf(ctx, d.ownerSch, o, d.lk)
		assign(db, o, d.name, pick(d.relT, rows, groups[normKey(k)]))
	}
	return nil
}

func (d *relDef) eagerMorphTo(ctx context.Context, db *migro.DB, owners []reflect.Value, scope scopeFn) error {
	ids := map[string][]any{}
	for _, o := range owners {
		typ, _ := fieldOf(ctx, d.ownerSch, o, d.typeCol)
		id, _ := fieldOf(ctx, d.ownerSch, o, d.fk)
		if blank(typ) || blank(id) {
			continue
		}
		alias := normKey(typ)
		ids[alias] = append(ids[alias], id)
	}
	found := map[string]reflect.Value{} // alias|id -> *Model
	for alias, list := range ids {
		t, ok := morphModel(alias)
		if !ok {
			return migro.Newf(migro.UnknownMorphType, "morph type %q is not registered: call orm.MorphMap", alias)
		}
		sch, err := parseModel(db, reflect.New(t).Interface())
		if err != nil {
			return err
		}
		pk, err := primaryKey(sch)
		if err != nil {
			return err
		}
		list, _ = uniqueKeys(list)
		rows, err := findRows(relQuery(ctx, db, t, scope).Where(inClause(db, pk), list), t, "load "+d.name)
		if err != nil {
			return err
		}
		for i := 0; i < rows.Len(); i++ {
			v, _ := fieldOf(ctx, sch, rows.Index(i), pk)
			found[alias+"|"+normKey(v)] = rows.Index(i).Addr()
		}
	}
	for _, o := range owners {
		typ, _ := fieldOf(ctx, d.ownerSch, o, d.typeCol)
		id, _ := fieldOf(ctx, d.ownerSch, o, d.fk)
		sl := reflect.MakeSlice(reflect.TypeFor[[]any](), 0, 1)
		if ptr, ok := found[normKey(typ)+"|"+normKey(id)]; ok {
			sl = reflect.Append(sl, ptr)
		}
		assign(db, o, d.name, sl)
	}
	return nil
}

// ---- counts -----------------------------------------------------------------------------------

// ---- constraints for lazy queries and existence checks ----------------------------------------

func (d *relDef) constrain(ctx context.Context, db *migro.DB, g *gorm.DB, owner reflect.Value) (*gorm.DB, error) {
	q := db.Dialect.Quote
	fresh := func() *gorm.DB { return db.Gorm.Session(&gorm.Session{NewDB: true}).WithContext(ctx) }
	key := func(col string) (any, error) { return fieldOf(ctx, d.ownerSch, owner, col) }
	switch d.kind {
	case kHasOne, kHasMany, kMorphOne, kMorphMany:
		k, err := key(d.lk)
		if err != nil {
			return nil, err
		}
		g = g.Where(q(d.fk)+" = ?", k)
		if d.typeCol != "" {
			g = g.Where(q(d.typeCol)+" = ?", d.typeVal)
		}
		if d.ofMany != nil {
			cond, args := d.ofMany.cond(db, d)
			g = g.Where(cond, args...)
		}
	case kBelongsTo:
		k, err := key(d.fk)
		if err != nil {
			return nil, err
		}
		g = g.Where(q(d.lk)+" = ?", k)
	case kBelongsToMany, kMorphToMany, kMorphedByMany:
		k, err := key(d.lk)
		if err != nil {
			return nil, err
		}
		sub := fresh().Table(d.pivot.table).Select(d.pivot.relatedCol).Where(q(d.pivot.ownerCol)+" = ?", k)
		if d.pivot.typeCol != "" {
			sub = sub.Where(q(d.pivot.typeCol)+" = ?", d.pivot.typeVal)
		}
		sub = d.pivot.constrain(db, sub)
		g = g.Where(q(d.rk)+" IN (?)", sub)
	case kThrough:
		k, err := key(d.lk)
		if err != nil {
			return nil, err
		}
		sub := fresh().Model(reflect.New(d.thSch.ModelType).Interface()).Select(d.thKey).Where(q(d.thFK)+" = ?", k)
		g = g.Where(q(d.relFK)+" IN (?)", sub)
	default:
		return nil, migro.Newf(migro.UnsupportedFeature, "relation %s cannot be queried directly", d.name)
	}
	return g, nil
}

func relatedQuery[T any](m *relMeta) (*Query[T], error) {
	if err := m.unbound(); err != nil {
		return nil, err
	}
	d, _, err := resolveByName(m.db, m.ownerValue().Type(), m.name)
	if err != nil {
		return nil, err
	}
	g, err := d.constrain(context.Background(), m.db, m.db.Gorm.Model(new(T)), m.ownerValue())
	if err != nil {
		return nil, err
	}
	q := &Query[T]{db: m.db, g: g, skip: &skipSet{}}
	if scopes := globalScopesOf(reflect.TypeFor[T]()); len(scopes) > 0 {
		q.g = q.g.Scopes(scopeFunc(scopes, q.skip))
	}
	return q, nil
}

// existsCond returns the owner column and a subquery of the owner keys that have a related row.
func (d *relDef) existsCond(db *migro.DB, scope scopeFn) (string, *gorm.DB, error) {
	q := db.Dialect.Quote
	fresh := func() *gorm.DB { return db.Gorm.Session(&gorm.Session{NewDB: true}) }
	related := func(col string) *gorm.DB {
		g := fresh().Model(reflect.New(d.relT).Interface()).Select(col)
		if scope != nil {
			g = scope(g)
		}
		return g
	}
	owner := func(col string) string { return q(d.ownerSch.Table) + "." + q(col) }
	switch d.kind {
	case kHasOne, kHasMany, kMorphOne, kMorphMany:
		sub := related(d.fk).Where(q(d.fk) + " IS NOT NULL")
		if d.typeCol != "" {
			sub = sub.Where(q(d.typeCol)+" = ?", d.typeVal)
		}
		return owner(d.lk), sub, nil
	case kBelongsTo:
		return owner(d.fk), related(d.lk), nil
	case kBelongsToMany, kMorphToMany, kMorphedByMany:
		sub := fresh().Table(d.pivot.table).Select(d.pivot.ownerCol).Where(q(d.pivot.ownerCol)+" IS NOT NULL").
			Where(q(d.pivot.relatedCol)+" IN (?)", related(d.rk))
		if d.pivot.typeCol != "" {
			sub = sub.Where(q(d.pivot.typeCol)+" = ?", d.pivot.typeVal)
		}
		sub = d.pivot.constrain(db, sub)
		return owner(d.lk), sub, nil
	case kThrough:
		sub := fresh().Model(reflect.New(d.thSch.ModelType).Interface()).Select(d.thFK).Where(q(d.thFK)+" IS NOT NULL").
			Where(q(d.thKey)+" IN (?)", related(d.relFK))
		return owner(d.lk), sub, nil
	}
	return "", nil, migro.Newf(migro.UnsupportedFeature, "has/whereHas is not supported on %s", d.name)
}

// ---- Query integration ------------------------------------------------------------------------

type withSpec struct {
	path  string
	scope scopeFn
}

func (w withSpec) rank() int {
	if w.scope != nil {
		return 1
	}
	return 0
}

// With eager-loads relations by field name; "Posts.Comments" nests. An optional scope constrains
// the last relation of the path: With("Posts", func(q *gorm.DB) *gorm.DB { return q.Where(...) }).
func (q *Query[T]) With(relation string, scope ...scopeFn) *Query[T] {
	spec := withSpec{path: relation}
	if len(scope) > 0 {
		spec.scope = scope[0]
	}
	q.with = append(q.with, spec)
	return q
}

// WithCount adds "<relation>_count" to each model's Extras (Eloquent's withCount).
func (q *Query[T]) WithCount(relations ...string) *Query[T] {
	q.counts = append(q.counts, relations...)
	return q
}

func (q *Query[T]) has(relation string, negate bool, scope []scopeFn) *Query[T] {
	d, _, err := resolveByName(q.db, reflect.TypeFor[T](), relation)
	if err != nil {
		_ = q.g.AddError(err)
		return q
	}
	var sc scopeFn
	if len(scope) > 0 {
		sc = scope[0]
	}
	col, sub, err := d.existsCond(q.db, sc)
	if err != nil {
		_ = q.g.AddError(err)
		return q
	}
	if negate {
		q.g = q.g.Where("("+col+" IS NULL OR "+col+" NOT IN (?))", sub)
		return q
	}
	q.g = q.g.Where(col+" IN (?)", sub)
	return q
}

// Has keeps models that have at least one related row; WhereHas also constrains those rows.
func (q *Query[T]) Has(relation string) *Query[T] { return q.has(relation, false, nil) }
func (q *Query[T]) WhereHas(relation string, scope scopeFn) *Query[T] {
	return q.has(relation, false, []scopeFn{scope})
}

// DoesntHave keeps models with no related row; WhereDoesntHave ignores rows outside scope.
func (q *Query[T]) DoesntHave(relation string) *Query[T] { return q.has(relation, true, nil) }
func (q *Query[T]) WhereDoesntHave(relation string, scope scopeFn) *Query[T] {
	return q.has(relation, true, []scopeFn{scope})
}

// hydrate binds fetched rows and runs the queued eager loads and counts.
func (q *Query[T]) hydrate(ctx context.Context, rows []T) error {
	bindAll(q.db, rows, true)
	if len(rows) == 0 || len(q.with)+len(q.counts)+len(q.aggs) == 0 {
		return nil
	}
	owners := make([]reflect.Value, len(rows))
	for i := range rows {
		owners[i] = reflect.ValueOf(&rows[i]).Elem()
	}
	// constrained loads first, so an unconstrained path under the same relation does not undo them
	specs := slices.SortedStableFunc(slices.Values(q.with), func(a, b withSpec) int {
		return cmp.Compare(b.rank(), a.rank())
	})
	done := map[string]bool{}
	for _, w := range specs {
		if err := loadPathOnce(ctx, q.db, owners, w.path, w.scope, "", done); err != nil {
			return err
		}
	}
	for _, name := range q.counts {
		if err := aggregateOnto(ctx, q.db, owners, aggSpec{rel: name, fn: "COUNT"}); err != nil {
			return err
		}
	}
	for _, a := range q.aggs {
		if err := aggregateOnto(ctx, q.db, owners, a); err != nil {
			return err
		}
	}
	return nil
}
