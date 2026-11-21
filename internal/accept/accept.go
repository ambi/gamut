// Package accept reads and writes acceptances of equivalent mutants in gamut.accepted.json at the module root.
// Acceptances are recorded in one place, without changing the source, by position-independent identifier and reason.
package accept

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/mutant"
)

// FileName is the name of the acceptance record at the module root.
const FileName = "gamut.accepted.json"

const version = 1

// Entry is the acceptance of one mutant.
type Entry struct {
	Reason string `json:"reason"`
}

// File is the acceptance record.
type File struct {
	path    string
	Version int               `json:"version"`
	Mutants map[string]*Entry `json:"mutants"`
}

// Load reads the acceptance record in the module root dir, or returns an empty record when there is none.
func Load(dir string) (*File, error) {
	f := &File{path: filepath.Join(dir, FileName), Version: version, Mutants: map[string]*Entry{}}
	data, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(f); err != nil {
		return nil, fmt.Errorf("%s: %w", f.path, err)
	}
	if f.Version != version {
		return nil, fmt.Errorf("%s: unsupported version %d", f.path, f.Version)
	}
	if f.Mutants == nil {
		f.Mutants = map[string]*Entry{}
	}
	return f, nil
}

// Path is the path of the record file.
func (f *File) Path() string { return f.path }

// Reason returns the reason id was accepted for, or empty when it is not accepted.
func (f *File) Reason(id string) string {
	if e := f.Mutants[id]; e != nil {
		return e.Reason
	}
	return ""
}

// Add accepts ids with reason, replacing the reason of those already accepted.
func (f *File) Add(ids []string, reason string) error {
	if reason = strings.TrimSpace(reason); reason == "" {
		return errors.New("a reason is required")
	}
	for _, id := range ids {
		if !wellFormed(id) {
			return fmt.Errorf("%q is not a mutant ID (file::TYPE::scope::hash)", id)
		}
	}
	for _, id := range ids {
		f.Mutants[id] = &Entry{Reason: reason}
	}
	return nil
}

// Remove removes the acceptances of ids.
func (f *File) Remove(ids []string) {
	for _, id := range ids {
		delete(f.Mutants, id)
	}
}

// Save writes the record, in identifier order with one line per field so diffs read easily.
// It writes a temporary file in the same directory and renames it, so an interruption never corrupts the previous file.
func (f *File) Save() error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".gamut-accepted-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), f.path)
}

// IDs returns the accepted identifiers in order.
func (f *File) IDs() []string {
	var out []string
	for id := range f.Mutants {
		out = append(out, id)
	}
	return slices.Sorted(slices.Values(out))
}

// InPackages reports whether the file of id lies directly in one of the module-relative package directories dirs.
// When only some packages are judged, it keeps acceptances of other packages from being reported as gone.
func InPackages(id string, dirs []string) bool {
	file, _, _ := strings.Cut(id, "::")
	return slices.Contains(dirs, path.Dir(file))
}

func wellFormed(id string) bool {
	parts := strings.Split(id, "::")
	return len(parts) >= 4 && parts[0] != "" && slices.Contains(mutant.Known, mutant.Type(parts[1])) && parts[len(parts)-1] != ""
}
