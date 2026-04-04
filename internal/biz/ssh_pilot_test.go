package biz

import (
	"reflect"
	"testing"
)

func TestNormalizeUserCustomCommands(t *testing.T) {
	got, err := normalizeUserCustomCommands([]string{" kubectl ", "htop", "kubectl", ""})
	if err != nil {
		t.Fatalf("normalizeUserCustomCommands returned error: %v", err)
	}

	want := []string{"kubectl", "htop"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected normalized commands, got=%v want=%v", got, want)
	}
}

func TestNormalizeUserCustomCommandsRejectInvalid(t *testing.T) {
	_, err := normalizeUserCustomCommands([]string{"bad command"})
	if err == nil {
		t.Fatalf("expected error for command containing spaces")
	}
}

func TestValidateWhitelistCommandWithCustomAllowed(t *testing.T) {
	allowed := allowedCommandSet([]string{"kubectl"})
	used, err := validateWhitelistCommand("kubectl get pods | grep api", allowed)
	if err != nil {
		t.Fatalf("validateWhitelistCommand returned error: %v", err)
	}

	want := []string{"kubectl", "grep"}
	if !reflect.DeepEqual(used, want) {
		t.Fatalf("unexpected validated commands, got=%v want=%v", used, want)
	}
}
