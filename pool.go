package migro

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"time"

	"github.com/LouisRiverstone/migro/dialect"
	"gorm.io/gorm"
)

// Pool mirrors Knex's pool options on top of database/sql.
type Pool struct {
	// Max open connections (Knex pool.max). 0 = unlimited.
	Max int
	// Min connections kept warm (Knex pool.min). Best effort: connections are opened at Open
	// and kept idle, but database/sql may still close them after IdleTimeout or MaxLifetime.
	Min int
	// AcquireTimeout bounds the wait for a free connection (Knex acquireConnectionTimeout).
	// Exceeding it returns POOL_ACQUIRE_TIMEOUT. 0 = wait until the context ends.
	AcquireTimeout time.Duration
	// IdleTimeout closes connections idle longer than this (Knex idleTimeoutMillis).
	IdleTimeout time.Duration
	// MaxLifetime recycles connections older than this.
	MaxLifetime time.Duration
	// Validate pings a pooled connection on checkout and discards it when the ping fails
	// (Knex pool.validate). Only effective for drivers implementing driver.Pinger.
	Validate bool
	// AfterCreate runs once for every new physical connection (Knex pool.afterCreate),
	// e.g. SET TIME ZONE or PRAGMA statements. An error discards the connection.
	AfterCreate func(ctx context.Context, conn Conn) error

	maxIdle int // from the deprecated Config.MaxIdleConns
}

// Conn is what an AfterCreate hook can do with a fresh connection.
type Conn interface {
	Exec(ctx context.Context, query string) error
}

func (p Pool) validate() error {
	switch {
	case p.Max < 0, p.Min < 0, p.AcquireTimeout < 0, p.IdleTimeout < 0, p.MaxLifetime < 0:
		return Newf(InvalidConfig, "pool values must not be negative")
	case p.Max > 0 && p.Min > p.Max:
		return Newf(InvalidConfig, "pool.min (%d) exceeds pool.max (%d)", p.Min, p.Max)
	}
	return nil
}

// advanced reports whether the pool needs the custom connector path.
func (p Pool) advanced() bool { return p.AfterCreate != nil || p.AcquireTimeout > 0 || p.Validate }

func (p Pool) apply(db *sql.DB) {
	if p.Max > 0 {
		db.SetMaxOpenConns(p.Max)
	}
	if idle := max(p.Min, p.maxIdle); idle > 0 {
		db.SetMaxIdleConns(max(idle, 2))
	}
	if p.IdleTimeout > 0 {
		db.SetConnMaxIdleTime(p.IdleTimeout)
	}
	if p.MaxLifetime > 0 {
		db.SetConnMaxLifetime(p.MaxLifetime)
	}
}

// warm opens Min connections at once so they land in the idle pool.
func (p Pool) warm(ctx context.Context, db *sql.DB) error {
	if p.Min <= 0 {
		return nil
	}
	conns := make([]*sql.Conn, 0, p.Min)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for range p.Min {
		c, err := db.Conn(ctx)
		if err != nil {
			return Wrap(ConnectionFailed, err, "warm pool")
		}
		conns = append(conns, c)
	}
	return nil
}

// ---- advanced path: custom connector -------------------------------------------------

type acquireKey struct{}

type acquireState struct {
	acquired atomic.Bool
	timer    *time.Timer
}

func markAcquired(ctx context.Context) {
	if st, ok := ctx.Value(acquireKey{}).(*acquireState); ok && st.acquired.CompareAndSwap(false, true) {
		st.timer.Stop()
	}
}

type connector struct {
	inner driver.Connector
	pool  Pool
}

func (c *connector) Driver() driver.Driver { return c.inner.Driver() }

func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if c.pool.AfterCreate != nil {
		if err := c.pool.AfterCreate(ctx, rawConn{conn}); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	markAcquired(ctx)
	return &hookedConn{Conn: conn, validate: c.pool.Validate}, nil
}

// hookedConn marks acquisition and optionally validates on checkout.
type hookedConn struct {
	driver.Conn
	validate bool
}

func (h *hookedConn) ResetSession(ctx context.Context) error {
	if h.validate {
		if p, ok := h.Conn.(driver.Pinger); ok {
			if err := p.Ping(ctx); err != nil {
				return driver.ErrBadConn
			}
		}
	}
	if r, ok := h.Conn.(driver.SessionResetter); ok {
		if err := r.ResetSession(ctx); err != nil {
			return err
		}
	}
	markAcquired(ctx)
	return nil
}

// Forward optional driver interfaces database/sql probes for.
func (h *hookedConn) IsValid() bool {
	if v, ok := h.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (h *hookedConn) Ping(ctx context.Context) error {
	if p, ok := h.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (h *hookedConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	if p, ok := h.Conn.(driver.ConnPrepareContext); ok {
		return p.PrepareContext(ctx, q)
	}
	return h.Prepare(q)
}

func (h *hookedConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	if b, ok := h.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, o)
	}
	return h.Conn.Begin() //nolint:staticcheck // fallback for drivers without ConnBeginTx
}

func (h *hookedConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	e, ok := h.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return e.ExecContext(ctx, q, args)
}

func (h *hookedConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	qr, ok := h.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return qr.QueryContext(ctx, q, args)
}

// CheckNamedValue forwards argument conversion to the driver (pgx and mysql rely on it).
func (h *hookedConn) CheckNamedValue(nv *driver.NamedValue) error {
	if c, ok := h.Conn.(driver.NamedValueChecker); ok {
		return c.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// rawConn lets AfterCreate run SQL on the driver connection.
type rawConn struct{ driver.Conn }

func (r rawConn) Exec(ctx context.Context, query string) error {
	if e, ok := r.Conn.(driver.ExecerContext); ok {
		_, err := e.ExecContext(ctx, query, nil)
		if !errors.Is(err, driver.ErrSkip) {
			return err
		}
	}
	st, err := r.Prepare(query)
	if err != nil {
		return err
	}
	defer st.Close()
	if sc, ok := st.(driver.StmtExecContext); ok {
		_, err = sc.ExecContext(ctx, nil)
		return err
	}
	_, err = st.Exec(nil) //nolint:staticcheck // fallback for drivers without StmtExecContext
	return err
}

// timedPool derives a context carrying the acquire deadline; database/sql consults it while waiting.
type timedPool struct {
	db      *sql.DB
	timeout time.Duration
}

var errAcquire = errors.New("pool acquire timeout")

func (p *timedPool) derive(parent context.Context) (context.Context, context.Context) {
	if p.timeout <= 0 {
		return parent, parent
	}
	ctx, cancel := context.WithCancelCause(parent)
	st := &acquireState{}
	st.timer = time.AfterFunc(p.timeout, func() { cancel(errAcquire) })
	return context.WithValue(ctx, acquireKey{}, st), ctx
}

func (p *timedPool) mapErr(cancelCtx context.Context, err error) error {
	if err != nil && errors.Is(context.Cause(cancelCtx), errAcquire) {
		return Wrap(PoolAcquireTimeout, err, "no free connection within %s", p.timeout)
	}
	return err
}

func (p *timedPool) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	c, cc := p.derive(ctx)
	st, err := p.db.PrepareContext(c, q)
	return st, p.mapErr(cc, err)
}

func (p *timedPool) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c, cc := p.derive(ctx)
	r, err := p.db.ExecContext(c, q, args...)
	return r, p.mapErr(cc, err)
}

func (p *timedPool) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c, cc := p.derive(ctx)
	r, err := p.db.QueryContext(c, q, args...)
	return r, p.mapErr(cc, err)
}

func (p *timedPool) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c, _ := p.derive(ctx)
	return p.db.QueryRowContext(c, q, args...)
}

func (p *timedPool) BeginTx(ctx context.Context, o *sql.TxOptions) (*sql.Tx, error) {
	c, cc := p.derive(ctx)
	tx, err := p.db.BeginTx(c, o)
	return tx, p.mapErr(cc, err)
}

// GetDBConn lets gorm's db.DB() return the underlying *sql.DB.
func (p *timedPool) GetDBConn() (*sql.DB, error) { return p.db, nil }

// poolHandle is a ready connection pool for a dialect.
type poolHandle struct {
	sqlDB *sql.DB
	conn  gorm.ConnPool
}

// openPool builds the *sql.DB through a custom connector so hooks and acquire timeouts apply.
func openPool(ctx context.Context, d dialect.Dialect, dsn string, p Pool) (*poolHandle, error) {
	name := d.DriverName()
	dsn = d.DriverDSN(dsn)
	probe, err := sql.Open(name, dsn) // lazy: does not connect
	if err != nil {
		return nil, Wrap(ConnectionFailed, err, "open %s driver", name)
	}
	drv := probe.Driver()
	_ = probe.Close()

	var inner driver.Connector
	if dc, ok := drv.(driver.DriverContext); ok {
		inner, err = dc.OpenConnector(dsn)
		if err != nil {
			return nil, Wrap(ConnectionFailed, err, "open %s connector", name)
		}
	} else {
		inner = dsnConnector{drv: drv, dsn: dsn}
	}
	db := sql.OpenDB(&connector{inner: inner, pool: p})
	p.apply(db)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, Wrap(ConnectionFailed, err, "connect %s", d.Name())
	}
	if err := p.warm(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	var cp gorm.ConnPool = db
	if p.AcquireTimeout > 0 {
		cp = &timedPool{db: db, timeout: p.AcquireTimeout}
	}
	return &poolHandle{sqlDB: db, conn: cp}, nil
}

type dsnConnector struct {
	drv driver.Driver
	dsn string
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.drv.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.drv }
