package migro_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro"
)

func open(t *testing.T, cfg migro.Config) *migro.DB {
	t.Helper()
	cfg.Dialect = "sqlite"
	if cfg.DSN == "" {
		cfg.DSN = t.TempDir() + "/p.db"
	}
	db, err := migro.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPoolValidation(t *testing.T) {
	bad := []migro.Pool{{Max: -1}, {Min: 5, Max: 2}, {AcquireTimeout: -time.Second}}
	for _, p := range bad {
		_, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: t.TempDir() + "/x.db", Pool: p})
		if migro.CodeOf(err) != migro.InvalidConfig {
			t.Errorf("%+v: want INVALID_CONFIG, got %v", p, err)
		}
	}
	if _, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: ":memory:", ReplicaPolicy: "weird"}); migro.CodeOf(err) != migro.InvalidConfig {
		t.Errorf("bad policy: %v", err)
	}
}

func TestPoolMinWarmsConnections(t *testing.T) {
	db := open(t, migro.Config{Pool: migro.Pool{Min: 3, Max: 5}})
	sqlDB, _ := db.Gorm.DB()
	if got := sqlDB.Stats().Idle; got != 3 {
		t.Fatalf("want 3 idle connections, got %d", got)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 5 {
		t.Fatalf("max=%d", got)
	}
}

func TestAfterCreateRunsOnEveryConnection(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	created := 0
	db := open(t, migro.Config{Pool: migro.Pool{
		Max: 3, Min: 3,
		AfterCreate: func(ctx context.Context, c migro.Conn) error {
			mu.Lock()
			created++
			mu.Unlock()
			return c.Exec(ctx, "PRAGMA foreign_keys = ON")
		},
	}})
	if created < 3 {
		t.Fatalf("hook ran %d times, want >= 3", created)
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var v int
			if err := db.Gorm.WithContext(ctx).Raw("PRAGMA foreign_keys").Scan(&v).Error; err != nil || v != 1 {
				t.Errorf("pragma not applied: v=%d err=%v", v, err)
			}
		}()
	}
	wg.Wait()
}

func TestAfterCreateErrorFailsOpen(t *testing.T) {
	_, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: t.TempDir() + "/e.db", Pool: migro.Pool{
		AfterCreate: func(ctx context.Context, c migro.Conn) error { return c.Exec(ctx, "NOT VALID SQL") },
	}})
	if migro.CodeOf(err) != migro.ConnectionFailed {
		t.Fatalf("want CONNECTION_FAILED, got %v", err)
	}
}

func TestAcquireTimeout(t *testing.T) {
	ctx := context.Background()
	db := open(t, migro.Config{Pool: migro.Pool{Max: 1, AcquireTimeout: 150 * time.Millisecond}})

	// Hold the only connection inside a transaction.
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = db.Transaction(ctx, func(tx *migro.DB) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	start := time.Now()
	err := db.Gorm.WithContext(ctx).Exec("SELECT 1").Error
	if !errors.Is(err, migro.ErrPoolAcquireTimeout) {
		t.Fatalf("want POOL_ACQUIRE_TIMEOUT, got %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took too long: %s", d)
	}
	close(release)

	// After release, queries work again and are not affected by a stale timer.
	time.Sleep(300 * time.Millisecond)
	if err := db.Gorm.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func TestAcquireTimeoutDoesNotCancelRunningWork(t *testing.T) {
	ctx := context.Background()
	db := open(t, migro.Config{Pool: migro.Pool{Max: 2, AcquireTimeout: 100 * time.Millisecond}})
	// A transaction longer than the acquire timeout must survive: the timer only guards acquisition.
	err := db.Transaction(ctx, func(tx *migro.DB) error {
		time.Sleep(300 * time.Millisecond)
		return tx.Gorm.Exec("SELECT 1").Error
	})
	if err != nil {
		t.Fatalf("long tx was cancelled: %v", err)
	}
}

func TestValidateOption(t *testing.T) {
	db := open(t, migro.Config{Pool: migro.Pool{Max: 2, Validate: true}})
	for range 5 {
		if err := db.Gorm.Exec("SELECT 1").Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestReplicasRouteReadsAndWrites(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	primaryDSN, replicaDSN := dir+"/primary.db", dir+"/replica.db"

	// Seed the replica directly, simulating replicated data that differs from the primary.
	rep := open(t, migro.Config{DSN: replicaDSN})
	primary := open(t, migro.Config{DSN: primaryDSN})
	for name, db := range map[string]*migro.DB{"replica": rep, "primary": primary} {
		if err := db.Gorm.Exec("CREATE TABLE notes (body varchar(20))").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Gorm.Exec("INSERT INTO notes (body) VALUES (?)", "from-"+name).Error; err != nil {
			t.Fatal(err)
		}
	}
	_ = rep.Close()
	_ = primary.Close()

	db := open(t, migro.Config{DSN: primaryDSN, Replicas: []string{replicaDSN}, ReplicaPolicy: "round-robin"})
	read := func(h *migro.DB) string {
		var body string
		if err := h.Gorm.WithContext(ctx).Raw("SELECT body FROM notes ORDER BY body LIMIT 1").Scan(&body).Error; err != nil {
			t.Fatal(err)
		}
		return body
	}
	if got := read(db); got != "from-replica" {
		t.Fatalf("read should hit replica, got %q", got)
	}
	if got := read(db.WriteDB()); got != "from-primary" {
		t.Fatalf("WriteDB read should hit primary, got %q", got)
	}
	if err := db.Gorm.WithContext(ctx).Exec("INSERT INTO notes (body) VALUES ('w')").Error; err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.WriteDB().Gorm.Raw("SELECT count(*) FROM notes").Scan(&n).Error
	if n != 2 {
		t.Fatalf("write must land on primary, count=%d", n)
	}
	// Transactions use the primary.
	_ = db.Transaction(ctx, func(tx *migro.DB) error {
		if got := read(tx); got != "from-primary" {
			t.Errorf("tx read should hit primary, got %q", got)
		}
		return nil
	})
}
