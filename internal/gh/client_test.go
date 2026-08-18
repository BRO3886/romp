package gh

import "testing"

func TestOpenPRNumber(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		want int
	}{
		{"no open PR", "[]", 0},
		{"single PR", `[{"number":42}]`, 42},
		{"multiple PRs", `[{"number":7},{"number":9}]`, 7},
		{"malformed", "not json", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := openPRNumber(tc.out)
			if tc.out == "not json" {
				if err == nil {
					t.Fatal("openPRNumber = nil error, want malformed-input error")
				}
				return
			}
			if err != nil {
				t.Fatalf("openPRNumber: %v", err)
			}
			if got != tc.want {
				t.Errorf("openPRNumber(%q) = %d, want %d", tc.out, got, tc.want)
			}
		})
	}
}
