// Package cache は、変異体の判定を、行と列に依存しない鍵とともにファイルへ保存し、次の実行で再利用できるようにする。
// どの判定を再利用してよいかの規則は run が持つ。このパッケージは鍵の計算と保存だけを受け持つ。
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

// formatVersion はキャッシュファイルの形式の版である。形式を変えたら上げ、古いファイルを読み捨てる。
const formatVersion = 3

// Entry は、1 体の変異体の判定と、判定したときの鍵である。
type Entry struct {
	// Package は変異体を含むパッケージの import パスである。
	Package  string `json:"package"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Mode     string `json:"mode,omitempty"`
	KilledBy string `json:"killed_by,omitempty"`
	// Killer は、検出したトップレベルのテストの名前である。逆依存パッケージのテストは import パスを前に付ける。
	Killer string `json:"killer,omitempty"`
	// Matrix は、感染したテストをすべて実行した判定（kill matrix）かである。真なら Killers が検出したテストのすべてである。
	Matrix  bool     `json:"matrix,omitempty"`
	Killers []string `json:"killers,omitempty"`
	// Decl、Deps、Imports は Keys の同名のメソッドの値である。
	Decl    string `json:"decl"`
	Deps    string `json:"deps"`
	Imports string `json:"imports"`
	// Reached は変異箇所に到達したテスト、Ran は判定のために実行したテストである。どちらも名前の順に並ぶ。
	Reached []string `json:"reached,omitempty"`
	Ran     []string `json:"ran,omitempty"`
	// TestKeys は、Reached と Ran のテストの Keys.Test の値である。
	TestKeys map[string]string `json:"test_keys,omitempty"`
}

// Coverage は、1 つのテストバイナリのテストが、1 つの対象パッケージの変異箇所に到達し感染した記録である。
// 箇所と変異体は、書き換えのたびに変わる番号ではなく、行と列に依存しない鍵で書く。
type Coverage struct {
	// Imports と Init は、記録したときの対象パッケージの Keys.Imports と Keys.Init である。
	Imports string `json:"imports"`
	Init    string `json:"init"`
	// Levels は、記録したときの全変異箇所の書き換えの段階（schemata.Level）である。
	Levels map[string]int `json:"levels"`
	// Tests は、トップレベルのテストの名前から記録を引く。
	Tests map[string]*TestRecord `json:"tests"`
}

// TestRecord は、1 件のテストの被覆の記録と、それを測ったときの鍵である。
type TestRecord struct {
	// Key はテストの鍵（Keys.Test か RevDepKey）である。
	Key string `json:"key"`
	// Reach は、テストが入った関数の宣言と、到達した変異箇所を囲む宣言の Keys.ReachOf の値である。
	Reach []string `json:"reach,omitempty"`
	// Hits は到達した変異箇所の鍵、Infected は感染した変異体の鍵（mutant.Mutant.Key）である。
	Hits     []string `json:"hits,omitempty"`
	Infected []string `json:"infected,omitempty"`
	// Duration は、被覆の記録の実行時間（ナノ秒）である。
	Duration int64 `json:"duration"`
	// Deep は、深さの制限を超えてだけ到達した変異箇所の鍵である。到達には数えない。
	Deep []string `json:"deep,omitempty"`
	// Unrepeatable は、同じプロセスで 2 回目に失敗したかである。
	Unrepeatable bool `json:"unrepeatable,omitempty"`
}

// Viability は、1 体の変異体の構築可能性の判定と、判定したときの鍵（Keys.Viability）である。
type Viability struct {
	Package string `json:"package"`
	Key     string `json:"key"`
	Viable  bool   `json:"viable"`
}

// Equivalence は、1 体の生き残りの変異体のコンパイル結果が元と同じかと、比べたときの鍵（Keys.Viability）である。
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
}

// Store は、キャッシュファイルの内容を持つ。並行に使える。
type Store struct {
	path string
	env  string
	// Reset は、前のファイルの記録を読み捨てた理由である。読み捨てなければ空である。
	Reset string

	mu      sync.Mutex
	entries map[string]*Entry
	// coverage は、対象パッケージとテストバイナリのパッケージの組（pairKey）から引く。
	coverage map[string]*Coverage
	// viability は、変異体の識別子から構築可能性の判定を引く。
	viability map[string]*Viability
	// equivalence は、変異体の識別子から、コンパイル結果の比較を引く。
	equivalence map[string]*Equivalence
	// quick は、実行の引数と設定の鍵から、前回の実行の入力の指紋と報告を引く。
	quick map[string]*Quick
	// loadHints は、読み込みのパターンと設定の鍵から、前回の読み込みでエクスポートデータを求めたパッケージを引く。
	loadHints map[string][]string
}

func pairKey(target, binary string) string { return target + " " + binary }

// Open は path のキャッシュを読む。env が記録と違えば、記録をすべて捨てた空の Store を返す。
// ファイルがないか、壊れているか、形式の版が違うときも空の Store を返す。
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

// Each は、すべての判定の記録を f に渡す。順は決まらない。渡した記録を書き換えてはならない。f の中で Store を使ってはならない。
func (s *Store) Each(f func(id string, e *Entry)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.entries {
		f(id, e)
	}
}

// Get は id の記録を返す。なければ nil を返す。返した記録を書き換えてはならない。
func (s *Store) Get(id string) *Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[id]
}

// Put は id の記録を置き換える。
func (s *Store) Put(id string, e *Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = e
}

// Coverage は、対象パッケージ target の変異箇所について、パッケージ binary のテストの被覆の記録を返す。なければ nil を返す。
// 返した記録を書き換えてはならない。
func (s *Store) Coverage(target, binary string) *Coverage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.coverage[pairKey(target, binary)]
}

// PutCoverage は、target と binary の組の被覆の記録を置き換える。
func (s *Store) PutCoverage(target, binary string, c *Coverage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.coverage[pairKey(target, binary)] = c
}

// Viable は、識別子 id の変異体の構築可能性の記録が鍵 key のものなら、その判定を返す。2 つ目の戻り値は記録を使えたかである。
func (s *Store) Viable(id, key string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.viability[id]
	if v == nil || key == "" || v.Key != key {
		return false, false
	}
	return v.Viable, true
}

// PutViable は、識別子 id の変異体の構築可能性の判定を記録する。
func (s *Store) PutViable(id string, v *Viability) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viability[id] = v
}

// Equivalent は、識別子 id の変異体のコンパイル結果の比較が鍵 key のものなら、その結果を返す。2 つ目の戻り値は記録を使えたかである。
func (s *Store) Equivalent(id, key string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.equivalence[id]
	if v == nil || key == "" || v.Key != key {
		return false, false
	}
	return v.Equal, true
}

// PutEquivalent は、識別子 id の変異体のコンパイル結果の比較を記録する。
func (s *Store) PutEquivalent(id string, v *Equivalence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.equivalence[id] = v
}

// Inherit は、from の記録を to へ写す。to に記録があれば何もしない。ファイルの名前を変えたときに前の判定を引き継ぐ。
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

// Len は記録の数を返す。
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Prune は、packages に含まれるパッケージの記録のうち、live が偽を返す識別子の記録を捨てる。
// 消えた変異体の記録がたまらないようにする。ほかのパッケージの記録は残す。
// 被覆の記録は、判定のたびにパッケージごとに置き換わるので、ここでは捨てない。
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

// Save は記録をファイルへ書く。同じディレクトリの一時ファイルに書いてから改名するので、途中で止まっても前のファイルは壊れない。
func (s *Store) Save() (err error) {
	return s.save()
}

// Merge は、inputs のキャッシュファイル（シャードごとのもの）の記録を合わせて out に書く。
// どの入力も同じ形式の版と環境でなければならない。同じ変異体の判定が複数あれば、kill matrix の判定を、なければ後の入力のものを採る。
// 変更のない再実行の記録（quick）はシャードの指定を鍵に含むので、合わせた後には当たらないため書かない。
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
		Equivalence: s.equivalence, Quick: s.quick, LoadHints: s.loadHints})
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

// Environment は、すべての判定に影響する環境のハッシュを返す。
// Go の版と対象の環境、gamut の実行ファイル、モジュールの go.mod と go.sum、ワークスペースのファイル、settings（変異演算子、ビルドタグ、テストのフラグなど）を含める。
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
	// 開発中の gamut は版の文字列が変わらないので、実行ファイルの中身で区別する。
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
