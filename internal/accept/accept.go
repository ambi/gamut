// Package accept は、等価な変異体の受理を、モジュールのルートの gamut.accepted.json に読み書きする。
// 受理は、ソースを変えずに、位置に依存しない識別子と理由で一か所に記録する。
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

// FileName は、モジュールのルートに置く受理の記録の名前である。
const FileName = "gamut.accepted.json"

const version = 1

// Entry は 1 体の受理である。
type Entry struct {
	Reason string `json:"reason"`
}

// File は受理の記録である。
type File struct {
	path    string
	Version int               `json:"version"`
	Mutants map[string]*Entry `json:"mutants"`
}

// Load は、モジュールのルート dir の受理の記録を読む。なければ空の記録を返す。
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

// Path は記録のファイルのパスである。
func (f *File) Path() string { return f.path }

// Reason は、id を受理していればその理由を、していなければ空を返す。
func (f *File) Reason(id string) string {
	if e := f.Mutants[id]; e != nil {
		return e.Reason
	}
	return ""
}

// Add は、ids を理由 reason で受理する。既に受理していれば理由を置き換える。
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

// Remove は ids の受理を消す。
func (f *File) Remove(ids []string) {
	for _, id := range ids {
		delete(f.Mutants, id)
	}
}

// Save は記録を書く。差分で読みやすいよう、識別子の順に、項目ごとに行を分けて書く。
// 途中で止まっても前のファイルが壊れないよう、同じディレクトリの一時ファイルに書いてから改名する。
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

// IDs は受理した識別子を、識別子の順に返す。
func (f *File) IDs() []string {
	var out []string
	for id := range f.Mutants {
		out = append(out, id)
	}
	return slices.Sorted(slices.Values(out))
}

// InPackages は、識別子 id のファイルが、モジュール相対のパッケージディレクトリ dirs のどれかの直下にあるかを返す。
// 一部のパッケージだけを判定したとき、ほかのパッケージの受理を「存在しない」と誤って報告しないために使う。
func InPackages(id string, dirs []string) bool {
	file, _, _ := strings.Cut(id, "::")
	return slices.Contains(dirs, path.Dir(file))
}

func wellFormed(id string) bool {
	parts := strings.Split(id, "::")
	return len(parts) >= 4 && parts[0] != "" && slices.Contains(mutant.Known, mutant.Type(parts[1])) && parts[len(parts)-1] != ""
}
