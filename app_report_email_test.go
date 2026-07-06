package main

import "testing"

func TestReportEmailCommand(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		wantCmd  string
		wantArg0 string
	}{
		{name: "macOS", goos: "darwin", wantCmd: "open", wantArg0: "mailto:test@example.com"},
		{name: "windows", goos: "windows", wantCmd: "rundll32", wantArg0: "url.dll,FileProtocolHandler"},
		{name: "linux", goos: "linux", wantCmd: "xdg-open", wantArg0: "mailto:test@example.com"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, args := reportEmailCommand(tc.goos, "mailto:test@example.com")
			if cmd != tc.wantCmd {
				t.Fatalf("command = %q, want %q", cmd, tc.wantCmd)
			}
			if len(args) == 0 {
				t.Fatalf("expected at least one argument")
			}
			if args[0] != tc.wantArg0 {
				t.Fatalf("args[0] = %q, want %q", args[0], tc.wantArg0)
			}
		})
	}
}

func TestTrimForMailto(t *testing.T) {
	if got := trimForMailto("  hello  ", 20); got != "hello" {
		t.Fatalf("trimForMailto() = %q, want %q", got, "hello")
	}

	if got := trimForMailto("abcdef", 3); got != "abc" {
		t.Fatalf("trimForMailto() = %q, want %q", got, "abc")
	}
}

func TestEncodeMailtoQueryValue(t *testing.T) {
	got := encodeMailtoQueryValue("hello world")
	if got != "hello%20world" {
		t.Fatalf("encodeMailtoQueryValue() = %q, want %q", got, "hello%20world")
	}
}
