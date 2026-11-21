package run

import (
	"fmt"
	"strings"
)

// testFlagNames are the names of the flags the testing package defines with the -test. prefix.
// The prefix is added to those the user writes without it, as with go test.
var testFlagNames = map[string]bool{
	"bench": true, "benchmem": true, "benchtime": true, "blockprofile": true, "blockprofilerate": true,
	"coverprofile": true, "cpu": true, "cpuprofile": true, "fullpath": true,
	"fuzz": true, "fuzzcachedir": true, "fuzzminimizetime": true, "fuzztime": true, "fuzzworker": true,
	"gocoverdir": true, "memprofile": true, "memprofilerate": true, "mutexprofile": true, "mutexprofilefraction": true,
	"outputdir": true, "parallel": true, "short": true, "shuffle": true, "skip": true, "testlogfile": true,
	"timeout": true, "trace": true, "v": true,
}

// reservedTestFlags are the flags the user cannot pass, because gamut sets them per run.
var reservedTestFlags = map[string]bool{"run": true, "count": true, "failfast": true, "list": true, "paniconexit0": true}

// ParseTestFlags turns space-separated test flags into the form passed to a test binary.
// They can be written like -short, as with go test, and -test. is added to the flags of testing.
// Flags a test defines itself (such as -update) are passed as is. Quoting is not supported.
func ParseTestFlags(s string) ([]string, error) {
	var out []string
	for f := range strings.FieldsSeq(s) {
		if !strings.HasPrefix(f, "-") {
			// The value of the preceding flag.
			out = append(out, f)
			continue
		}
		body := strings.TrimLeft(f, "-")
		name, _, _ := strings.Cut(body, "=")
		name = strings.TrimPrefix(name, "test.")
		if reservedTestFlags[name] {
			return nil, fmt.Errorf("test flag %s is set by gamut for each run and cannot be passed", f)
		}
		if testFlagNames[name] {
			body = "test." + strings.TrimPrefix(body, "test.")
		}
		out = append(out, "-"+body)
	}
	return out, nil
}
