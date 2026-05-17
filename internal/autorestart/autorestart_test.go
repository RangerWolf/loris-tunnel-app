package autorestart

import (
	"testing"
)

func TestIsWailsDevEnvironment(t *testing.T) {
	t.Setenv("devserver", "1")
	if !IsWailsDevEnvironment() {
		t.Fatal("expected dev when devserver set")
	}
	t.Setenv("devserver", "")
	if IsWailsDevEnvironment() {
		t.Fatal("expected prod when devserver unset")
	}
	t.Setenv("devserver", "   ")
	if IsWailsDevEnvironment() {
		t.Fatal("whitespace-only devserver treated as unset")
	}
}

func TestRelaunchDetached_DevNoOp(t *testing.T) {
	t.Setenv("devserver", "1")
	if err := RelaunchDetached(); err != nil {
		t.Fatal(err)
	}
}
