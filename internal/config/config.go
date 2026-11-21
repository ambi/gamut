// Package config reads the config file gamut.json at the module root and turns it into the values of the equivalent flags.
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

// FileName is the name of the config file looked for at the module root.
const FileName = "gamut.json"

// Config is the contents of the config file. Omitted keys leave the flag defaults alone.
type Config struct {
	// Path is the path of the file read.
	Path string `json:"-"`
	// Mutators are the mutators to enable, as with -only.
	Mutators []string `json:"mutators,omitempty"`
	// ExcludeFiles are regular expressions of module-relative paths of files not to mutate, as with -exclude-files.
	ExcludeFiles []string `json:"exclude_files,omitempty"`
	// Timeout is the factor and minimum of the timeout, as with -timeout-factor and -timeout-min.
	Timeout *Timeout `json:"timeout,omitempty"`
	// Workers is as with -workers.
	Workers *int `json:"workers,omitempty"`
	// Tags are build tags, as with -tags.
	Tags []string `json:"tags,omitempty"`
	// TestFlags are flags passed to test binaries, as with -test-flags.
	TestFlags []string `json:"test_flags,omitempty"`
	// Critical are globs of module-relative paths of critical region files, as with -critical.
	Critical []string `json:"critical,omitempty"`
	// Thresholds are the score thresholds, as with -threshold-high, -threshold-low, and -threshold-break.
	Thresholds *Thresholds `json:"thresholds,omitempty"`
	// FailOnSurvived is as with -fail-on-survived.
	FailOnSurvived *bool `json:"fail_on_survived,omitempty"`
	// Arid adds calls not to mutate, as with -arid-call and -arid-package.
	Arid *Arid `json:"arid,omitempty"`
}

// Arid are the calls and packages added to the arid rules.
type Arid struct {
	// Calls are globs of functions and methods ("<package>.<Func>", "<package>.<Type>.<Method>").
	Calls []string `json:"calls,omitempty"`
	// Packages are import paths, including the packages below them.
	Packages []string `json:"packages,omitempty"`
}

// Thresholds are the score thresholds (percentages).
type Thresholds struct {
	High  *float64 `json:"high,omitempty"`
	Low   *float64 `json:"low,omitempty"`
	Break *float64 `json:"break,omitempty"`
}

// Timeout configures the timeout.
type Timeout struct {
	Factor *float64 `json:"factor,omitempty"`
	// Min is in the form of Go's time.ParseDuration (such as "2s").
	Min *string `json:"min,omitempty"`
}

// Find reads the config file in the module root dir, or returns nil when there is none.
func Find(dir string) (*Config, error) {
	path := filepath.Join(dir, FileName)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return Load(path)
}

// Load reads the config file at path and checks the values. Unknown keys are errors.
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
		if !slices.Contains(mutant.Known, mutant.Type(name)) {
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
	if t := c.Thresholds; t != nil {
		for name, v := range map[string]*float64{"high": t.High, "low": t.Low, "break": t.Break} {
			if v != nil && (*v < 0 || *v > 100) {
				return fmt.Errorf("thresholds.%s: must be between 0 and 100", name)
			}
		}
		if t.High != nil && t.Low != nil && *t.Low > *t.High {
			return errors.New("thresholds.low: must not exceed thresholds.high")
		}
	}
	if a := c.Arid; a != nil {
		if err := (&mutant.Arid{Calls: a.Calls}).Validate(); err != nil {
			return fmt.Errorf("arid.calls: %w", err)
		}
	}
	return nil
}

// Flags maps the names of the equivalent flags to the values of the keys that are set.
// Repeatable flags (-exclude-files) have several values. Omitted keys are not included.
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
	if len(c.Critical) > 0 {
		out["critical"] = slices.Clone(c.Critical)
	}
	if t := c.Thresholds; t != nil {
		for name, v := range map[string]*float64{"threshold-high": t.High, "threshold-low": t.Low, "threshold-break": t.Break} {
			if v != nil {
				out[name] = []string{strconv.FormatFloat(*v, 'g', -1, 64)}
			}
		}
	}
	if c.FailOnSurvived != nil {
		out["fail-on-survived"] = []string{strconv.FormatBool(*c.FailOnSurvived)}
	}
	if a := c.Arid; a != nil {
		if len(a.Calls) > 0 {
			out["arid-call"] = slices.Clone(a.Calls)
		}
		if len(a.Packages) > 0 {
			out["arid-package"] = slices.Clone(a.Packages)
		}
	}
	return out
}

// Items returns the names of the top-level keys of the config file in name order, for checking against the JSON Schema.
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
