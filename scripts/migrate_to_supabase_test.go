package main

import "testing"

func TestRequireDBPassword_UnsetFails(t *testing.T) {
	t.Setenv("SUPABASE_DB_PASSWORD", "")
	if _, err := requireDBPassword(); err == nil {
		t.Fatal("expected error when SUPABASE_DB_PASSWORD is unset")
	}
}

func TestRequireDBPassword_ReadsEnv(t *testing.T) {
	t.Setenv("SUPABASE_DB_PASSWORD", "dummy-value")
	got, err := requireDBPassword()
	if err != nil || got != "dummy-value" {
		t.Fatalf("got %q, %v", got, err)
	}
}
