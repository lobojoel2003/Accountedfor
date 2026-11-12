// Package testdb gives integration tests a fresh PostgreSQL database.
//
// Tests that need a database are skipped unless
// ACCOUNTEDFOR_TEST_DATABASE_URL points at a server where the user may
// create databases, for example:
//
//	ACCOUNTEDFOR_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable
package testdb

import (
	"context"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lobojoel2003/accountedfor/internal/store"
)

// EnvVar names the environment variable with the server URL.
const EnvVar = "ACCOUNTEDFOR_TEST_DATABASE_URL"

var (
	mu     sync.Mutex
	stores = map[string]*store.Store{}
)

// Open returns a migrated, empty store backed by the database
// af_test_<name>. Each test package uses its own name so packages can run in
// parallel. The database is created once per process and emptied on every
// call.
func Open(t testing.TB, name string) *store.Store {
	t.Helper()
	base := os.Getenv(EnvVar)
	if base == "" {
		t.Skipf("set %s to run database tests", EnvVar)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	mu.Lock()
	defer mu.Unlock()
	st, ok := stores[name]
	if !ok {
		admin, err := pgx.Connect(ctx, base)
		if err != nil {
			t.Fatalf("connect to %s: %v", EnvVar, err)
		}
		dbname := pgx.Identifier{"af_test_" + name}.Sanitize()
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+dbname+" WITH (FORCE)"); err != nil {
			t.Fatalf("drop test database: %v", err)
		}
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbname); err != nil {
			t.Fatalf("create test database: %v", err)
		}
		_ = admin.Close(ctx)

		u, err := url.Parse(base)
		if err != nil {
			t.Fatalf("parse %s: %v", EnvVar, err)
		}
		u.Path = "/af_test_" + name
		st, err = store.Open(ctx, u.String())
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		if err := st.Migrate(ctx); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		stores[name] = st
	}
	if err := st.Reset(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	store.Domain = "test.example.org"
	return st
}
