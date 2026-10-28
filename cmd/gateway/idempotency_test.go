package main

import (
	"strings"
	"testing"
)

func TestResolveIdempotency(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		name    string
		env     map[string]string
		want    idemMode
		wantErr string
	}{
		{"redis addr selects redis", map[string]string{"REDIS_ADDR": "127.0.0.1:6379"}, idemRedis, ""},
		{"explicit redis", map[string]string{"IDEMPOTENCY": "redis", "REDIS_ADDR": "r:6379"}, idemRedis, ""},
		{"explicit memory", map[string]string{"IDEMPOTENCY": "memory"}, idemMemory, ""},
		{"memory wins over addr when explicit", map[string]string{"IDEMPOTENCY": "memory", "REDIS_ADDR": "r:6379"}, idemMemory, ""},
		{"nothing configured fails", map[string]string{}, "", "no idempotency store configured"},
		{"redis without addr fails", map[string]string{"IDEMPOTENCY": "redis"}, "", "requires REDIS_ADDR"},
		{"unknown value fails", map[string]string{"IDEMPOTENCY": "etcd", "REDIS_ADDR": "r:6379"}, "", "unknown IDEMPOTENCY"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveIdempotency(env(tc.env))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
