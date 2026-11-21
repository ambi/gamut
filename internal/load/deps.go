package load

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type depPackage struct {
	ImportPath      string
	Dir             string
	GoFiles         []string
	TestGoFiles     []string
	XTestGoFiles    []string
	CgoFiles        []string
	EmbedFiles      []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string
	Deps            []string
	Module          *struct {
		Main    bool
		Replace *struct{ Version string }
	}
}

// local reports whether the package's source may change locally: it is in the main module (workspaces included)
// or behind a replace to a directory without a version.
// go.mod and go.sum fix the contents of other modules, and the Go version fixes the standard library.
func (p *depPackage) local() bool {
	return p.Module != nil && (p.Module.Main || p.Module.Replace != nil && p.Module.Replace.Version == "")
}

// SourceDeps returns, per target package, the absolute paths of the files that may affect its verdicts, except the package's own Go and test files:
// the Go and embedded files of local packages the test binary depends on, the target's embedded files, and the files in its testdata directory.
// It runs go list -deps -test to include packages only tests import. It builds no export data.
func SourceDeps(pkgs []*Package) (map[string][]string, error) {
	if len(pkgs) == 0 {
		return map[string][]string{}, nil
	}
	patterns := make([]string, len(pkgs))
	for i, p := range pkgs {
		patterns[i] = p.ImportPath
	}
	deps, err := ListDeps(pkgs[0].ModuleDir, pkgs[0].Tags, patterns)
	if err != nil {
		return nil, err
	}
	return deps.Sources(pkgs)
}

// Deps is the result of go list -deps -test.
type Deps struct {
	listed map[string]*depPackage
}

// ListDeps runs go list -deps -test on patterns in the module directory dir.
// It takes no Package, so it can run alongside Load. patterns should name the same packages as those given to Load.
func ListDeps(dir string, tags, patterns []string) (*Deps, error) {
	listed, err := listTestDeps(dir, tags, patterns)
	if err != nil {
		return nil, err
	}
	return &Deps{listed: listed}, nil
}

// Sources computes the same as SourceDeps from the list d, which must include every package of pkgs.
func (d *Deps) Sources(pkgs []*Package) (map[string][]string, error) {
	listed := d.listed
	result := map[string][]string{}
	for _, pkg := range pkgs {
		if listed[pkg.ImportPath] == nil {
			return nil, fmt.Errorf("%s is not in the dependency list", pkg.ImportPath)
		}
		files := map[string]bool{}
		add := func(dir string, names ...[]string) {
			for _, name := range slices.Concat(names...) {
				files[filepath.Join(dir, name)] = true
			}
		}
		if self := listed[pkg.ImportPath]; self != nil {
			add(self.Dir, self.EmbedFiles, self.TestEmbedFiles, self.XTestEmbedFiles)
		}
		// p.test holds the test binary's dependencies. Without test files, p's own are used.
		root := listed[pkg.ImportPath+".test"]
		if root == nil {
			root = listed[pkg.ImportPath]
		}
		if root != nil {
			for _, d := range root.Deps {
				// A dependency recompiled for tests reads "q [p.test]"; its files are those of q.
				base, _, _ := strings.Cut(d, " ")
				if base == pkg.ImportPath || base == pkg.ImportPath+"_test" {
					continue
				}
				if dep := listed[base]; dep != nil && dep.local() {
					add(dep.Dir, dep.GoFiles, dep.CgoFiles, dep.EmbedFiles)
				}
			}
		}
		testdata, err := regularFiles(filepath.Join(pkg.Dir, "testdata"))
		if err != nil {
			return nil, err
		}
		for _, f := range testdata {
			files[f] = true
		}
		result[pkg.ImportPath] = slices.Sorted(maps.Keys(files))
	}
	return result, nil
}

// listTestDeps runs go list -deps -test on patterns in dir and returns a table from import path to package.
func listTestDeps(dir string, tags, patterns []string) (map[string]*depPackage, error) {
	args := []string{"list", "-e", "-deps", "-test",
		"-json=ImportPath,Dir,GoFiles,CgoFiles,TestGoFiles,XTestGoFiles,EmbedFiles,TestEmbedFiles,XTestEmbedFiles,Deps,Module"}
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}
	cmd := exec.Command("go", append(args, patterns...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -test in %s: %w: %s", cmd.Dir, err, strings.TrimSpace(stderr.String()))
	}
	listed := map[string]*depPackage{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p depPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		listed[p.ImportPath] = &p
	}
	return listed, nil
}

// RevDep is a package in the module, with tests, that imports a target package transitively.
type RevDep struct {
	ImportPath string
	// Dir is the absolute path of the package directory.
	Dir string
	// TestFiles are the absolute paths of the test files (external tests included).
	TestFiles []string
	// Files are the absolute paths of the files in the test key:
	// the package's own Go, test, and embedded files and the files under testdata, and
	// the Go and embedded files of local packages the test binary depends on (except the target).
	Files []string
}

// ReverseDeps returns, per target package, the packages in the module with tests whose test binaries link it.
// It runs go list -deps -test on every package of the main module and walks the test binaries' dependencies backwards.
// The target itself is not included. The result is in import path order.
func ReverseDeps(pkgs []*Package) (map[string][]RevDep, error) {
	result := map[string][]RevDep{}
	if len(pkgs) == 0 {
		return result, nil
	}
	listed, err := listTestDeps(pkgs[0].ModuleDir, pkgs[0].Tags, []string{"./..."})
	if err != nil {
		return nil, err
	}
	targets := map[string]bool{}
	for _, p := range pkgs {
		targets[p.ImportPath] = true
	}
	for name, test := range listed {
		q, ok := strings.CutSuffix(name, ".test")
		self := listed[q]
		if !ok || self == nil || self.Module == nil || !self.Module.Main || len(self.TestGoFiles)+len(self.XTestGoFiles) == 0 {
			continue
		}
		deps := map[string]bool{}
		for _, d := range test.Deps {
			base, _, _ := strings.Cut(d, " ")
			deps[base] = true
		}
		for target := range targets {
			if target == q || !deps[target] {
				continue
			}
			r := RevDep{ImportPath: q, Dir: self.Dir}
			for _, n := range slices.Concat(self.TestGoFiles, self.XTestGoFiles) {
				r.TestFiles = append(r.TestFiles, filepath.Join(self.Dir, n))
			}
			files := map[string]bool{}
			add := func(dir string, names ...[]string) {
				for _, n := range slices.Concat(names...) {
					files[filepath.Join(dir, n)] = true
				}
			}
			add(self.Dir, self.GoFiles, self.CgoFiles, self.TestGoFiles, self.XTestGoFiles, self.EmbedFiles, self.TestEmbedFiles, self.XTestEmbedFiles)
			for d := range deps {
				if dep := listed[d]; d != target && d != q && d != q+"_test" && dep != nil && dep.local() {
					add(dep.Dir, dep.GoFiles, dep.CgoFiles, dep.EmbedFiles)
				}
			}
			testdata, err := regularFiles(filepath.Join(self.Dir, "testdata"))
			if err != nil {
				return nil, err
			}
			for _, f := range testdata {
				files[f] = true
			}
			r.Files = slices.Sorted(maps.Keys(files))
			result[target] = append(result[target], r)
		}
	}
	for _, rs := range result {
		slices.SortFunc(rs, func(a, b RevDep) int { return strings.Compare(a.ImportPath, b.ImportPath) })
	}
	return result, nil
}

// regularFiles returns the regular files under dir, or nothing when dir does not exist.
func regularFiles(dir string) ([]string, error) {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}
