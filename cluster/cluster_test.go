package cluster_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/cluster"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

func open(t *testing.T) *cluster.Cluster {
	t.Helper()
	dir := t.TempDir()
	cfgs := map[string]migro.Config{}
	for _, n := range []string{"s1", "s2", "s3"} {
		cfgs[n] = migro.Config{Dialect: "sqlite", DSN: fmt.Sprintf("%s/%s.db", dir, n)}
	}
	c, err := cluster.Open(context.Background(), cfgs, cluster.HashRouter([]string{"s1", "s2", "s3"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRoutingIsStableAndSpreads(t *testing.T) {
	c := open(t)
	seen := map[string]int{}
	for i := range 300 {
		a, err := c.For(i)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := c.For(i)
		if a != b {
			t.Fatalf("key %d routed to different shards", i)
		}
		for _, n := range c.Names() {
			if s, _ := c.Shard(n); s == a {
				seen[n]++
			}
		}
	}
	for n, cnt := range seen {
		if cnt < 50 {
			t.Errorf("shard %s got only %d/300 keys", n, cnt)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("expected all shards used: %v", seen)
	}
	if _, err := c.For(nil); migro.CodeOf(err) != migro.ShardNotFound {
		t.Fatalf("nil key: %v", err)
	}
	if _, err := c.Shard("zzz"); !errors.Is(err, &migro.Error{Code: migro.ShardNotFound}) {
		t.Fatalf("unknown shard: %v", err)
	}
}

func TestMigrateAllAndEachFailure(t *testing.T) {
	ctx := context.Background()
	c := open(t)
	ms := []migrate.Migration{{
		Name: "001_users",
		Up: func(ctx context.Context, s *schema.Builder) error {
			return s.CreateTable(ctx, "users", func(t *schema.Table) { t.Increments("id"); t.String("name") })
		},
	}}
	res, err := c.Migrate(ctx, ms)
	if err != nil || len(res) != 3 {
		t.Fatalf("%v %v", res, err)
	}
	for n, r := range res {
		if len(r.Names) != 1 {
			t.Errorf("shard %s: %+v", n, r)
		}
	}

	// Data lands only on the routed shard.
	db, _ := c.For(42)
	db.Gorm.Exec("INSERT INTO users (name) VALUES ('x')")
	total := 0
	_ = c.Each(ctx, func(_ context.Context, _ string, d *migro.DB) error {
		var n int64
		d.Gorm.Table("users").Count(&n)
		if n > 0 {
			total += int(n)
		}
		return nil
	})
	if total != 1 {
		t.Fatalf("row should exist on exactly one shard, total=%d", total)
	}

	err = c.Each(ctx, func(_ context.Context, name string, _ *migro.DB) error {
		if name == "s2" {
			return errors.New("down")
		}
		return nil
	})
	if migro.CodeOf(err) != migro.ShardFailed {
		t.Fatalf("want SHARD_FAILED, got %v", err)
	}
}

func TestConfigErrors(t *testing.T) {
	if _, err := cluster.New(nil, cluster.HashRouter(nil)); migro.CodeOf(err) != migro.InvalidConfig {
		t.Fatal(err)
	}
	if _, err := cluster.Open(context.Background(), map[string]migro.Config{"a": {Dialect: "nope"}}, cluster.HashRouter([]string{"a"})); err == nil {
		t.Fatal("bad shard config should fail")
	}
}
