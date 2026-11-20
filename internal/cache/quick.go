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

// Quick は、変更のない再実行で、読み込みも型検査もビルドも省くための、前回の実行の入力の指紋と報告である。
// 入力のファイルの大きさと更新時刻、ディレクトリの中身の一覧が前回と同じなら、前回の報告をそのまま返す。
type Quick struct {
	// Fingerprint は、Files、Dirs、Trees の状態の指紋である。
	Fingerprint string `json:"fingerprint"`
	// Files は状態を比べるファイル、Dirs は直下のファイルの一覧と状態を比べるディレクトリ、
	// Trees は下のすべてのディレクトリを比べる木（<ディレクトリ>/... のパターンの根）である。
	Files []string `json:"files,omitempty"`
	Dirs  []string `json:"dirs,omitempty"`
	Trees []string `json:"trees,omitempty"`
	// Report は、前回の実行の JSON の報告である。
	Report json.RawMessage `json:"report"`
}

// Fingerprint は、files の状態、dirs の直下のファイルの一覧と状態と下位ディレクトリの名前、trees の下のすべてのファイルの一覧と状態の指紋を返す。
// 状態は大きさと更新時刻で、中身は読まない。ないファイルも「ない」という状態として含める。
// 木の下の、名前が . で始まるディレクトリと node_modules は、go のパターンが見ないので含めない。
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
				// 新しい testdata などのディレクトリを捉える。
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

// Quick は、実行の引数と設定の鍵 key の前回の Quick を返す。なければ nil を返す。
func (s *Store) Quick(key string) *Quick {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quick[key]
}

// PutQuick は、key の Quick を置き換える。
func (s *Store) PutQuick(key string, q *Quick) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quick[key] = q
}

// QuickKey は、実行の引数と設定から Quick の鍵を作る。
func QuickKey(parts ...string) string {
	return hashStrings(parts)
}

// LoadHint は、読み込みの鍵 key の、前回の読み込みでエクスポートデータを求めたパッケージの import パスを返す。
// 読み込みは、これを依存の一覧と並行して求める（load.WithHint）。
func (s *Store) LoadHint(key string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadHints[key]
}

// PutLoadHint は、key の読み込みの hint を置き換える。
func (s *Store) PutLoadHint(key string, hint []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadHints[key] = hint
}
