package forward

import (
	"testing"
	"time"

	"loris-tunnel/internal/model"
)

func TestDialTimeoutFromJumper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		timeoutMs int
		want      time.Duration
	}{
		{name: "default", timeoutMs: 0, want: 5 * time.Second},
		{name: "negative", timeoutMs: -1, want: 5 * time.Second},
		{name: "custom", timeoutMs: 8000, want: 8 * time.Second},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := dialTimeoutFromJumper(model.Jumper{TimeoutMs: tt.timeoutMs})
			if got != tt.want {
				t.Fatalf("dialTimeoutFromJumper() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDialTimeoutFromJumpers(t *testing.T) {
	t.Parallel()

	if got := dialTimeoutFromJumpers(nil); got != 5*time.Second {
		t.Fatalf("empty jumpers = %v, want 5s", got)
	}

	jumpers := []model.Jumper{
		{TimeoutMs: 3000},
		{TimeoutMs: 7000},
	}
	if got := dialTimeoutFromJumpers(jumpers); got != 7*time.Second {
		t.Fatalf("last jumper timeout = %v, want 7s", got)
	}
}
