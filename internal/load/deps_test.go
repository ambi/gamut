package load_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/ambi/gamut/internal/load"
)

func TestSourceDeps_ListsLocalPackagesTheTestBinaryUses(t *testing.T) {
	pkgs, err := load.Load([]string{"../../testdata/multi/..."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := load.SourceDeps(pkgs)
	if err != nil {
		t.Fatal(err)
	}
	rel := func(path string) string {
		r, err := filepath.Rel(pkgs[0].ModuleDir, path)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.ToSlash(r)
	}
	got := map[string][]string{}
	for path, files := range deps {
		for _, f := range files {
			got[path] = append(got[path], rel(f))
		}
	}
	// b は a を import する。自分の Go ファイルとテストファイルは鍵が別に持つので含めない。
	want := map[string][]string{"multi/a": nil, "multi/b": {"a/a.go"}, "multi/c": nil}
	for path, w := range want {
		if !slices.Equal(got[path], w) {
			t.Errorf("%s: %v, want %v", path, got[path], w)
		}
	}
}
