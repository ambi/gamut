// Package config は、モジュールのルートの設定ファイル gamut.json を読み、同じ意味のフラグの値に直す。
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ambi/gamut/internal/mutant"
)

// FileName は、モジュールのルートで探す設定ファイルの名前である。
const FileName = "gamut.json"

// Config は設定ファイルの中身である。省いた項目は、フラグの既定値のままにする。
type Config struct {
	// Path は読んだファイルのパスである。
	Path string `json:"-"`
	// Mutators は有効にする変異演算子で、-only と同じ意味である。
	Mutators []string `json:"mutators,omitempty"`
	// ExcludeFiles は、変異の対象から外すファイルのモジュール相対のパスの正規表現で、-exclude-files と同じ意味である。
	ExcludeFiles []string `json:"exclude_files,omitempty"`
	// Timeout は時間切れの上限の係数と下限で、-timeout-factor と -timeout-min と同じ意味である。
	Timeout *Timeout `json:"timeout,omitempty"`
	// Workers は -workers と同じ意味である。
	Workers *int `json:"workers,omitempty"`
	// Tags はビルドタグで、-tags と同じ意味である。
	Tags []string `json:"tags,omitempty"`
	// TestFlags はテストバイナリに渡すフラグで、-test-flags と同じ意味である。
	TestFlags []string `json:"test_flags,omitempty"`
}

// Timeout は時間切れの上限の設定である。
type Timeout struct {
	Factor *float64 `json:"factor,omitempty"`
	// Min は Go の time.ParseDuration の形（"2s" など）である。
	Min *string `json:"min,omitempty"`
}

// Find は、モジュールのルート dir の設定ファイルを読む。なければ nil を返す。
func Find(dir string) (*Config, error) {
	path := filepath.Join(dir, FileName)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return Load(path)
}

// Load は、path の設定ファイルを読み、項目の値を確かめる。未知の項目は誤りにする。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: invalid config: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: invalid config: data after the top-level object", path)
	}
	c.Path = path
	if err := c.check(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func (c *Config) check() error {
	for _, name := range c.Mutators {
		if !slices.Contains(mutant.AllTypes, mutant.Type(name)) {
			return fmt.Errorf("mutators: unknown mutator type %q", name)
		}
	}
	for _, re := range c.ExcludeFiles {
		if _, err := regexp.Compile(re); err != nil {
			return fmt.Errorf("exclude_files: %w", err)
		}
	}
	if t := c.Timeout; t != nil {
		if t.Factor != nil && *t.Factor <= 0 {
			return errors.New("timeout.factor: must be positive")
		}
		if t.Min != nil {
			if _, err := time.ParseDuration(*t.Min); err != nil {
				return fmt.Errorf("timeout.min: %w", err)
			}
		}
	}
	if c.Workers != nil && *c.Workers < 1 {
		return errors.New("workers: must be at least 1")
	}
	return nil
}

// Flags は、設定した項目を、同じ意味のフラグの名前から値への対応にする。
// 繰り返し指定できるフラグ（-exclude-files）は値を複数持つ。省いた項目は含めない。
func (c *Config) Flags() map[string][]string {
	out := map[string][]string{}
	if len(c.Mutators) > 0 {
		out["only"] = []string{strings.Join(c.Mutators, ",")}
	}
	if len(c.ExcludeFiles) > 0 {
		out["exclude-files"] = slices.Clone(c.ExcludeFiles)
	}
	if t := c.Timeout; t != nil {
		if t.Factor != nil {
			out["timeout-factor"] = []string{strconv.FormatFloat(*t.Factor, 'g', -1, 64)}
		}
		if t.Min != nil {
			d, _ := time.ParseDuration(*t.Min)
			out["timeout-min"] = []string{d.String()}
		}
	}
	if c.Workers != nil {
		out["workers"] = []string{strconv.Itoa(*c.Workers)}
	}
	if len(c.Tags) > 0 {
		out["tags"] = []string{strings.Join(c.Tags, ",")}
	}
	if len(c.TestFlags) > 0 {
		out["test-flags"] = []string{strings.Join(c.TestFlags, " ")}
	}
	return out
}

// Items は、設定ファイルの最上位の項目の名前を、名前の順に返す。JSON Schema との一致を確かめるのに使う。
func Items() []string {
	var out []string
	t := reflect.TypeFor[Config]()
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "-" {
			out = append(out, name)
		}
	}
	return slices.Sorted(slices.Values(out))
}
