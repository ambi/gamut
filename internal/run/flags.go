package run

import (
	"fmt"
	"strings"
)

// testFlagNames は、testing パッケージが -test. の接頭辞付きで定義するフラグの名前である。
// 利用者が go test と同じく接頭辞なしで書いたものに、接頭辞を補う。
var testFlagNames = map[string]bool{
	"bench": true, "benchmem": true, "benchtime": true, "blockprofile": true, "blockprofilerate": true,
	"coverprofile": true, "cpu": true, "cpuprofile": true, "fullpath": true,
	"fuzz": true, "fuzzcachedir": true, "fuzzminimizetime": true, "fuzztime": true, "fuzzworker": true,
	"gocoverdir": true, "memprofile": true, "memprofilerate": true, "mutexprofile": true, "mutexprofilefraction": true,
	"outputdir": true, "parallel": true, "short": true, "shuffle": true, "skip": true, "testlogfile": true,
	"timeout": true, "trace": true, "v": true,
}

// reservedTestFlags は、gamut が実行ごとに決めるので、利用者が渡せないフラグである。
var reservedTestFlags = map[string]bool{"run": true, "count": true, "failfast": true, "list": true, "paniconexit0": true}

// ParseTestFlags は、空白で区切ったテストのフラグを、テストバイナリへ渡す形にする。
// go test と同じく -short のように書け、testing のフラグには -test. を補う。
// テストが自分で定義したフラグ（-update など）はそのまま渡す。引用符には対応しない。
func ParseTestFlags(s string) ([]string, error) {
	var out []string
	for f := range strings.FieldsSeq(s) {
		if !strings.HasPrefix(f, "-") {
			// 直前のフラグの値である。
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
