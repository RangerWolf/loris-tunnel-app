package biz

import (
	"testing"
)

func TestSplitSemicolonOutsideQuotes(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"ps aux", []string{"ps aux"}},
		{"a;b;c", []string{"a", "b", "c"}},
		{`echo "a;b"`, []string{`echo "a;b"`}},
		{`awk '{a;b}'`, []string{`awk '{a;b}'`}},
		{
			`awk '{sum+=$1} END{print sum " KB"; print sum/1024 " MB"}'; echo x`,
			[]string{`awk '{sum+=$1} END{print sum " KB"; print sum/1024 " MB"}'`, "echo x"},
		},
	}
	for _, tc := range cases {
		got := splitSemicolonOutsideQuotes(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("splitSemicolonOutsideQuotes(%q): len %d, want %d: %#v", tc.in, len(got), len(tc.want), got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("splitSemicolonOutsideQuotes(%q)[%d]=%q want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestValidateWhitelistCommand_nginxDiagChain(t *testing.T) {
	allowed := allowedCommandSet(nil)
	cmd := `ps -C nginx -o pid,user,%mem,%cpu,rss,cmd --no-headers --sort=-rss; echo "----TOTAL RSS----"; ps -C nginx -o rss= | awk '{sum+=$1} END{print sum " KB"; print sum/1024 " MB"}'; echo "----SYSTEM MEM----"; free -m; echo "----NGINX STATUS----"; systemctl status nginx --no-pager`
	used, err := validateWhitelistCommand(cmd, allowed)
	if err != nil {
		t.Fatalf("validateWhitelistCommand: %v", err)
	}
	want := []string{"ps", "echo", "ps", "awk", "echo", "free", "echo", "systemctl"}
	if len(used) != len(want) {
		t.Fatalf("used=%v want %v", used, want)
	}
	for i := range want {
		if used[i] != want[i] {
			t.Fatalf("used[%d]=%q want %q", i, used[i], want[i])
		}
	}
}

func TestValidateWhitelistCommand_rejectsUnlistedAfterSemicolon(t *testing.T) {
	allowed := allowedCommandSet(nil)
	_, err := validateWhitelistCommand(`ps aux; rm -rf /`, allowed)
	if err == nil {
		t.Fatal("expected error for rm")
	}
}
