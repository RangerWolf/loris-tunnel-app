package license

import "testing"

func TestResolveBaseURLByBuildType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		buildType string
		want      string
	}{
		{name: "dev", buildType: "dev", want: devBackendAPIBaseURL},
		{name: "dev uppercase", buildType: "DEV", want: devBackendAPIBaseURL},
		{name: "production", buildType: "production", want: prodBackendAPIBaseURL},
		{name: "debug", buildType: "debug", want: prodBackendAPIBaseURL},
		{name: "empty", buildType: "", want: prodBackendAPIBaseURL},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveBaseURLByBuildType(tt.buildType)
			if got != tt.want {
				t.Fatalf("resolveBaseURLByBuildType(%q) = %q, want %q", tt.buildType, got, tt.want)
			}
		})
	}
}
