// Package cache saves mutant verdicts to a file with keys independent of lines and columns, so the next run can reuse them.
// The rules for which verdicts may be reused belong to run. This package only computes the keys and stores them.
package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// formatVersion is the version of the cache file format. Raise it when the format changes; old files are discarded.
const formatVersion = 3

// Entry is one mutant's verdict and the keys at the time of judging.
type Entry struct {
	// Package is the import path of the package containing the mutant.
	Package  string `json:"package"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Mode     string `json:"mode,omitempty"`
	KilledBy string `json:"killed_by,omitempty"`
	// Killer is the name of the detecting top-level test. Tests of reverse dependencies are prefixed with their import path.
	Killer string `json:"killer,omitempty"`
	// Matrix reports whether the verdict ran every infected test (kill matrix). When true, Killers are all the detecting tests.
	Matrix  bool     `json:"matrix,omitempty"`
	Killers []string `json:"killers,omitempty"`
	// Decl, Deps, and Imports are the values of the Keys methods of the same names.
	Decl    string `json:"decl"`
	Deps    string `json:"deps"`
	Imports string `json:"imports"`
	// Reached are the tests that reached the mutation site and Ran the tests run for judging, both in name order.
	Reached []string `json:"reached,omitempty"`
	Ran     []string `json:"ran,omitempty"`
	// TestKeys are the Keys.Test values of the tests in Reached and Ran.
	TestKeys map[string]string `json:"test_keys,omitempty"`
}

// Coverage records the tests of one test binary reaching and infecting the mutation sites of one target package.
// Sites and mutants are written with keys independent of lines and columns, not with the numbers that change with every rewrite.
type Coverage struct {
	// Imports and Init are the target package's Keys.Imports and Keys.Init at recording time.
	Imports string `json:"imports"`
	Init    string `json:"init"`
	// Levels are the rewrite levels (schemata.Level) of all mutation sites at recording time.
	Levels map[string]int `json:"levels"`
	// Tests maps the names of top-level tests to their records.
	Tests map[string]*TestRecord `json:"tests"`
}

// TestRecord is one test's coverage record and the key at the time of measuring.
type TestRecord struct {
	// Key is the test's key (Keys.Test or RevDepKey).
	Key string `json:"key"`
	// Reach are the Keys.ReachOf values of the declarations of functions the test entered and of those enclosing reached mutation sites.
	Reach []string `json:"reach,omitempty"`
	// Hits are the keys of the reached mutation sites, and Infected the keys of the infected mutants (mutant.Mutant.Key).
	Hits     []string `json:"hits,omitempty"`
	Infected []string `json:"infected,omitempty"`
	// Duration is the running time of coverage recording, in nanoseconds.
	Duration int64 `json:"duration"`
	// Deep are the keys of mutation sites reached only beyond the depth limit. They do not count as reached.
	Deep []string `json:"deep,omitempty"`
	// Unrepeatable reports whether the test failed on its second run in the same process.
	Unrepeatable bool `json:"unrepeatable,omitempty"`
}

// Viability is one mutant's viability verdict and the key at the time (Keys.Viability).
type Viability struct {
	Package string `json:"package"`
	Key     string `json:"key"`
	Viable  bool   `json:"viable"`
}

// Equivalence records whether one survivor compiles to the same code as the original, and the key at the time of comparing (Keys.Viability).
type Equivalence struct {
	Package string `json:"package"`
	Key     string `json:"key"`
	Equal   bool   `json:"equal"`
}

type fileFormat struct {
	Version     int                     `json:"version"`
	Env         string                  `json:"env"`
	Mutants     map[string]*Entry       `json:"mutants"`
	Coverage    map[string]*Coverage    `json:"coverage,omitempty"`
	Viability   map[string]*Viability   `json:"viability,omitempty"`
	Equivalence map[string]*Equivalence `json:"equivalence,omitempty"`
	Quick       map[string]*Quick       `json:"quick,omitempty"`
	LoadHints   map[string][]string     `json:"load_hints,omitempty"`
	// Last is the report (report.Report JSON) of the last run with this cache. gamut merge reads it to combine shards.
	Last json.RawMessage `json:"last,omitempty"`
}

// Store holds the contents of a cache file. It is safe for concurrent use.
type Store struct {
	path string
	env  string
	// Reset is why the previous file's records were discarded, or empty when they were not.
	Reset string

	mu      sync.Mutex
	entries map[string]*Entry
	// coverage is keyed by the pair of the target package and the test binary's package (pairKey).
	coverage map[string]*Coverage
	// viability maps mutant identifiers to viability verdicts.
	viability map[string]*Viability
	// equivalence maps mutant identifiers to compile comparisons.
	equivalence map[string]*Equivalence
	// quick maps keys of run arguments and settings to the previous run's input fingerprint and report.
	quick map[string]*Quick
	// loadHints maps keys of load patterns and settings to the packages whose export data the previous load requested.
	loadHints map[string][]string
	// last is the report of the last run.
	last json.RawMessage
}

func pairKey(target, binary string) string { return target + " " + binary }

// Open reads the cache at path. When env differs from the record, it returns an empty Store with every record discarded.
// It also returns an empty Store when the file is missing, corrupt, or of another format version.
func Open(path, env string) (*Store, error) {
	s := &Store{path: path, env: env, entries: map[string]*Entry{}, coverage: map[string]*Coverage{}, viability: map[string]*Viability{},
		equivalence: map[string]*Equivalence{}, quick: map[string]*Quick{}, loadHints: map[string][]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f fileFormat
	switch {
	case json.Unmarshal(data, &f) != nil:
		s.Reset = "the cache file is unreadable"
	case f.Version != formatVersion:
		s.Reset = fmt.Sprintf("the cache format changed from %d to %d", f.Version, formatVersion)
	case f.Env != env:
		s.Reset = "the environment changed (Go, gamut, go.mod, go.sum, mutators, tags, test flags, or timeouts)"
	default:
		for id, e := range f.Mutants {
			if e != nil {
				s.entries[id] = e
			}
		}
		for k, c := range f.Coverage {
			if c != nil {
				s.coverage[k] = c
			}
		}
		for k, q := range f.Quick {
			if q != nil {
				s.quick[k] = q
			}
		}
		maps.Copy(s.loadHints, f.LoadHints)
		s.last = f.Last
		for k, v := range f.Viability {
			if v != nil {
				s.viability[k] = v
			}
		}
		for k, v := range f.Equivalence {
			if v != nil {
				s.equivalence[k] = v
			}
		}
	}
	return s, nil
}

// Each passes every verdict record to f in no particular order. f must neither modify the records nor use the Store.
func (s *Store) Each(f func(id string, e *Entry)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.entries {
		f(id, e)
	}
}

// Get returns the record of id, or nil. The returned record must not be modified.
func (s *Store) Get(id string) *Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[id]
}

// Put replaces the record of id.
func (s *Store) Put(id string, e *Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = e
}

// Coverage returns the coverage record of the tests of package binary for the mutation sites of target, or nil.
// The returned record must not be modified.
func (s *Store) Coverage(target, binary string) *Coverage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.coverage[pairKey(target, binary)]
}

// PutCoverage replaces the coverage record of the pair target and binary.
func (s *Store) PutCoverage(target, binary string, c *Coverage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.coverage[pairKey(target, binary)] = c
}

// Viable returns the viability verdict of mutant id when its record has key key. The second result reports whether the record could be used.
func (s *Store) Viable(id, key string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.viability[id]
	if v == nil || key == "" || v.Key != key {
		return false, false
	}
	return v.Viable, true
}

// PutViable records the viability verdict of mutant id.
func (s *Store) PutViable(id string, v *Viability) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viability[id] = v
}

// Equivalent returns the compile comparison of mutant id when its record has key key. The second result reports whether the record could be used.
func (s *Store) Equivalent(id, key string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.equivalence[id]
	if v == nil || key == "" || v.Key != key {
		return false, false
	}
	return v.Equal, true
}

// PutEquivalent records the compile comparison of mutant id.
func (s *Store) PutEquivalent(id string, v *Equivalence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.equivalence[id] = v
}

// Inherit copies the records of from to to, unless to already has a record. It carries verdicts over a file rename.
func (s *Store) Inherit(from, to string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[from]
	if !ok || s.entries[to] != nil {
		return false
	}
	s.entries[to] = e
	return true
}

// Len returns the number of records.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Prune discards, among the records of the packages in packages, those whose identifiers live reports false,
// so records of removed mutants do not pile up. Records of other packages stay.
// Coverage records are replaced per package on every run, so they are not discarded here.
func (s *Store) Prune(packages []string, live func(id string) bool) {
	in := map[string]bool{}
	for _, p := range packages {
		in[p] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.entries {
		if in[e.Package] && !live(id) {
			delete(s.entries, id)
		}
	}
	for id, v := range s.viability {
		if in[v.Package] && !live(id) {
			delete(s.viability, id)
		}
	}
	for id, v := range s.equivalence {
		if in[v.Package] && !live(id) {
			delete(s.equivalence, id)
		}
	}
}

// Save writes the records to the file. It writes a temporary file in the same directory and renames it, so an interruption never corrupts the previous file.
func (s *Store) Save() (err error) {
	return s.save()
}

// PutLast records the report of the last run.
func (s *Store) PutLast(report json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = report
}

// ReadLast returns the report of the last run in the cache at path.
func ReadLast(path string) (json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("read the cache %s: %w", path, err)
	}
	if f.Version != formatVersion {
		return nil, fmt.Errorf("the cache %s has format %d, want %d", path, f.Version, formatVersion)
	}
	if len(f.Last) == 0 {
		return nil, fmt.Errorf("the cache %s has no report of a run; run gamut with -cache %s first", path, path)
	}
	return f.Last, nil
}

// Merge combines the records of the cache files inputs (one per shard) and writes them to out.
// Every input must have the same format version and environment. For a mutant judged more than once, the kill matrix verdict wins, otherwise the later input.
// Unchanged-rerun records (quick) include the shard in their key and would never match after merging, so they are not written.
func Merge(inputs []string, out string) error {
	merged := &Store{path: out, entries: map[string]*Entry{}, coverage: map[string]*Coverage{}, viability: map[string]*Viability{},
		equivalence: map[string]*Equivalence{}, quick: map[string]*Quick{}, loadHints: map[string][]string{}}
	for i, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			return err
		}
		var f fileFormat
		if err := json.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("read the cache %s: %w", in, err)
		}
		switch {
		case f.Version != formatVersion:
			return fmt.Errorf("the cache %s has format %d, want %d", in, f.Version, formatVersion)
		case i > 0 && f.Env != merged.env:
			return fmt.Errorf("the cache %s was written in another environment than %s (Go, gamut, go.mod, go.sum, or flags differ)", in, inputs[0])
		}
		merged.env = f.Env
		for id, e := range f.Mutants {
			if old := merged.entries[id]; e != nil && (old == nil || !old.Matrix || e.Matrix) {
				merged.entries[id] = e
			}
		}
		maps.Copy(merged.coverage, f.Coverage)
		maps.Copy(merged.viability, f.Viability)
		maps.Copy(merged.equivalence, f.Equivalence)
		maps.Copy(merged.loadHints, f.LoadHints)
	}
	return merged.save()
}

func (s *Store) save() (err error) {
	s.mu.Lock()
	data, err := json.Marshal(fileFormat{Version: formatVersion, Env: s.env, Mutants: s.entries, Coverage: s.coverage, Viability: s.viability,
		Equivalence: s.equivalence, Quick: s.quick, LoadHints: s.loadHints, Last: s.last})
	s.mu.Unlock()
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cache-*.json")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// Environment returns the hash of the environment that affects every verdict:
// the Go version and target platform, the gamut executable, the module's go.mod and go.sum, workspace files, and settings (mutators, build tags, test flags, and so on).
func Environment(moduleDir string, settings ...string) (string, error) {
	cmd := exec.Command("go", "env", "GOVERSION", "GOOS", "GOARCH", "GOFLAGS", "GOEXPERIMENT", "CGO_ENABLED", "GOWORK")
	cmd.Dir = moduleDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	goEnv, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go env: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	h := sha256.New()
	writeString(h, string(goEnv))
	// A development build of gamut keeps the same version string, so builds are told apart by the executable's contents.
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exeData, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	writeString(h, string(exeData))
	files := []string{filepath.Join(moduleDir, "go.mod"), filepath.Join(moduleDir, "go.sum")}
	lines := strings.Split(strings.TrimSpace(string(goEnv)), "\n")
	if work := strings.TrimSpace(lines[len(lines)-1]); work != "" && work != "off" {
		files = append(files, work, work+".sum")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		writeString(h, filepath.Base(f))
		writeString(h, string(data))
	}
	for _, s := range settings {
		writeString(h, s)
	}
	return digest(h), nil
}
