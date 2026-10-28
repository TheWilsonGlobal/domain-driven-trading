package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// D5: the store is chosen explicitly; memory only when asked for.
func TestResolveStore(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    storeMode
		wantErr string
	}{
		{"url set defaults to postgres", map[string]string{"DATABASE_URL": "postgres://x"}, storePostgres, ""},
		{"explicit postgres", map[string]string{"STORE": "postgres", "DATABASE_URL": "postgres://x"}, storePostgres, ""},
		{"explicit memory", map[string]string{"STORE": "memory"}, storeMemory, ""},
		{"memory wins over url only when explicit", map[string]string{"STORE": "MEMORY", "DATABASE_URL": "postgres://x"}, storeMemory, ""},
		{"nothing configured is an error, not memory", map[string]string{}, "", "set DATABASE_URL"},
		{"postgres without url", map[string]string{"STORE": "postgres"}, "", "requires DATABASE_URL"},
		{"unknown value", map[string]string{"STORE": "sqlite"}, "", "unknown STORE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveStore(env(c.env))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err=%v, want containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %q err=%v, want %q", got, err, c.want)
			}
		})
	}
}

// D5: an unreachable database fails with a clear error and never leaks the URL.
func TestConnectPool_UnreachableFailsWithoutLeakingURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := connectPool(ctx, "postgres://user:s3cretpw@127.0.0.1:1/db?connect_timeout=2")
	if err == nil {
		pool.Close()
		t.Fatal("expected an error for an unreachable database")
	}
	if !strings.Contains(err.Error(), "unreachable") || strings.Contains(err.Error(), "s3cretpw") {
		t.Fatalf("err=%v", err)
	}
	if _, err := connectPool(ctx, "not a url"); err == nil || strings.Contains(err.Error(), "not a url") {
		t.Fatalf("bad url err=%v", err)
	}
}
