package mutant_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ambi/gamut/internal/mutant"
)

func TestCritical_GlobsAndFunctionComments(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":         "module m\n\ngo 1.27.4\n",
		"auth/expiry.go": "package auth\n\nfunc Expired(now, exp int) bool { return now > exp }\n",
		"auth/fee.go": `package auth

func Plain(a, b int) int { return a + b }

// Fee は金額を計算する。
//
//gamut:critical
func Fee(a, b int) int { return a * b }

//gamut:critical
var _ = 1
`,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := loadOne(filepath.Join(dir, "auth"))
	if err != nil {
		t.Fatal(err)
	}
	ms := mutant.Enumerate(pkg, classic)
	scopes := func(set map[string]bool) []string {
		var out []string
		for _, m := range ms {
			if set[m.ID] && !slices.Contains(out, m.Scope) {
				out = append(out, m.Scope)
			}
		}
		slices.Sort(out)
		return out
	}
	cases := []struct {
		name  string
		globs []string
		want  []string
	}{
		{"comments only", nil, []string{"Fee"}},
		{"a file glob", []string{"auth/expiry.go"}, []string{"Expired", "Fee"}},
		{"a double-star glob", []string{"**/exp*.go"}, []string{"Expired", "Fee"}},
		{"a directory glob", []string{"auth/**"}, []string{"Expired", "Fee", "Plain"}},
		{"a glob that matches nothing", []string{"billing/**"}, []string{"Fee"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, warnings, err := mutant.Critical(pkg, ms, tc.globs)
			if err != nil {
				t.Fatal(err)
			}
			if got := scopes(set); !slices.Equal(got, tc.want) {
				t.Errorf("critical scopes %v, want %v", got, tc.want)
			}
			// 関数の Doc の外の //gamut:critical は警告する。
			if len(warnings) != 1 {
				t.Errorf("warnings %q, want one for the comment on a var", warnings)
			}
		})
	}
	if _, _, err := mutant.Critical(pkg, ms, []string{"auth/[x"}); err == nil {
		t.Error("a malformed glob was accepted")
	}
}
