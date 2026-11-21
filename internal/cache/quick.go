package cache

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Quick is the fingerprint of the previous run's inputs and its report, for skipping loading, type-checking, and building on an unchanged rerun.
// When the sizes and modification times of the input files and the listings of the directories match the previous run, the previous report is returned as is.
type Quick struct {
	// Fingerprint is the fingerprint of the state of Files, Dirs, and Trees.
	Fingerprint string `json:"fingerprint"`
	// Files are the files whose state is compared, Dirs the directories whose file listing and states are compared,
	// and Trees the trees whose every directory is compared (the roots of <directory>/... patterns).
	Files []string `json:"files,omitempty"`
	Dirs  []string `json:"dirs,omitempty"`
	Trees []string `json:"trees,omitempty"`
	// Report is the previous run's JSON report.
	Report json.RawMessage `json:"report"`
}

// Fingerprint returns the fingerprint of the states of files, the file listings, states, and subdirectory names of dirs, and the listings and states of every file under trees.
// The state is size and modification time; contents are not read. A missing file is included with the state "missing".
// Directories under the trees whose names start with . and node_modules are left out, as go patterns skip them.
func Fingerprint(files, dirs, trees []string) (string, error) {
	h := sha256.New()
	stat := func(path string) {
		writeString(h, path)
		info, err := os.Stat(path)
		if err != nil {
			writeString(h, "missing")
			return
		}
		writeInt(h, info.Size())
		writeInt(h, info.ModTime().UnixNano())
		writeInt(h, int64(info.Mode()))
	}
	for _, f := range slices.Sorted(slices.Values(files)) {
		stat(f)
	}
	for _, d := range slices.Sorted(slices.Values(dirs)) {
		entries, err := os.ReadDir(d)
		if err != nil {
			writeString(h, d+" missing")
			continue
		}
		for _, e := range entries {
			switch {
			case e.Type().IsRegular():
				stat(filepath.Join(d, e.Name()))
			case e.IsDir():
				// Catches new directories such as testdata.
				writeString(h, filepath.Join(d, e.Name())+"/")
			}
		}
	}
	for _, root := range slices.Sorted(slices.Values(trees)) {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					writeString(h, path+" missing")
					return nil
				}
				return err
			}
			if d.IsDir() {
				if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
					return filepath.SkipDir
				}
				writeString(h, path+"/")
				return nil
			}
			if d.Type().IsRegular() {
				stat(path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return digest(h), nil
}

// Quick returns the previous Quick for the key key of the run's arguments and settings, or nil.
func (s *Store) Quick(key string) *Quick {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quick[key]
}

// PutQuick replaces the Quick of key.
func (s *Store) PutQuick(key string, q *Quick) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quick[key] = q
}

// QuickKey builds the key of a Quick from the run's arguments and settings.
func QuickKey(parts ...string) string {
	return hashStrings(parts)
}

// LoadHint returns, for the load key key, the import paths of the packages whose export data the previous load requested.
// Loading requests them alongside the dependency list (load.WithHint).
func (s *Store) LoadHint(key string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadHints[key]
}

// PutLoadHint replaces the load hint of key.
func (s *Store) PutLoadHint(key string, hint []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadHints[key] = hint
}
