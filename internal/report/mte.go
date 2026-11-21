package report

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"

	"github.com/ambi/gamut/internal/run"
)

// MTEReport は、mutation-testing-elements のスキーマ 2（mutation-testing-report-schema）の報告である。
// このスキーマを読む HTML の閲覧画面やダッシュボードで、gamut の判定を表示できる。
type MTEReport struct {
	SchemaVersion string                  `json:"schemaVersion"`
	Thresholds    MTEThresholds           `json:"thresholds"`
	ProjectRoot   string                  `json:"projectRoot,omitempty"`
	Files         map[string]*MTEFile     `json:"files"`
	TestFiles     map[string]*MTETestFile `json:"testFiles,omitempty"`
	Framework     *MTEFramework           `json:"framework,omitempty"`
}

// MTEThresholds は閾値（整数の百分率）である。
type MTEThresholds struct {
	High int `json:"high"`
	Low  int `json:"low"`
}

// MTEFramework は報告を作ったツールである。
type MTEFramework struct {
	Name string `json:"name"`
}

// MTEFile は 1 ファイルの変異体である。
type MTEFile struct {
	Language string      `json:"language"`
	Source   string      `json:"source"`
	Mutants  []MTEMutant `json:"mutants"`
}

// MTETestFile は、テストの一覧である。gamut はテストのファイルを記録しないので、すべてのテストを名前のないファイルに入れる。
type MTETestFile struct {
	Tests []MTETest `json:"tests"`
}

// MTETest は 1 件のテストである。識別子にはテストの名前を使う。
type MTETest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MTEMutant は 1 体の変異体である。
type MTEMutant struct {
	ID           string      `json:"id"`
	MutatorName  string      `json:"mutatorName"`
	Replacement  string      `json:"replacement,omitempty"`
	Location     MTELocation `json:"location"`
	Status       string      `json:"status"`
	StatusReason string      `json:"statusReason,omitempty"`
	KilledBy     []string    `json:"killedBy,omitempty"`
	CoveredBy    []string    `json:"coveredBy,omitempty"`
}

// MTELocation は、変異した字句の範囲である。行と列は 1 から数え、end は含まない。
type MTELocation struct {
	Start MTEPosition `json:"start"`
	End   MTEPosition `json:"end"`
}

// MTEPosition は行と列である。
type MTEPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// MTE は、報告を mutation-testing-elements の形式にする。各ファイルの中身を moduleDir から読む。
//
// 状態の対応は、KILLED→Killed、LIVED→Survived、NOT COVERED→NoCoverage、NOT VIABLE→CompileError、TIMED OUT→Timeout、
// PENDING→Pending である。数えない変異体（無効化コメントで止めたもの、受理したもの、EQUIVALENT）は Ignored にし、理由を statusReason に入れる。
func (r *Report) MTE(moduleDir string) (*MTEReport, error) {
	out := &MTEReport{SchemaVersion: "2", ProjectRoot: moduleDir, Files: map[string]*MTEFile{}, Framework: &MTEFramework{Name: "gamut"}}
	t := Thresholds{High: 80, Low: 60}
	if r.Thresholds != nil {
		t = *r.Thresholds
	}
	out.Thresholds = MTEThresholds{High: int(math.Round(t.High)), Low: int(math.Round(t.Low))}
	tests := map[string]bool{}
	file := func(rel string) (*MTEFile, error) {
		if f := out.Files[rel]; f != nil {
			return f, nil
		}
		src, err := os.ReadFile(filepath.Join(moduleDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("read the source for the mutation-testing-elements report: %w", err)
		}
		f := &MTEFile{Language: "go", Source: string(src), Mutants: []MTEMutant{}}
		out.Files[rel] = f
		return f, nil
	}
	for _, f := range r.Files {
		mf, err := file(f.Path)
		if err != nil {
			return nil, err
		}
		for _, m := range f.Mutations {
			status, reason := mteStatus(m)
			mm := MTEMutant{
				ID: m.ID, MutatorName: m.Type, Replacement: m.Replacement, Status: status, StatusReason: reason,
				Location: span(m.Line, m.Column, m.Original), CoveredBy: m.Tests,
			}
			if m.KilledBy != "" {
				mm.KilledBy = []string{m.KilledBy}
			}
			for _, name := range append(slices.Clone(m.Tests), mm.KilledBy...) {
				tests[name] = true
			}
			mf.Mutants = append(mf.Mutants, mm)
		}
	}
	for _, ig := range r.Ignored {
		mf, err := file(ig.Path)
		if err != nil {
			return nil, err
		}
		mf.Mutants = append(mf.Mutants, MTEMutant{
			ID: ig.ID, MutatorName: ig.Type, Replacement: ig.Replacement, Status: "Ignored",
			StatusReason: "ignored by comment: " + ig.Reason, Location: span(ig.Line, ig.Column, ig.Original),
		})
	}
	if len(tests) > 0 {
		tf := &MTETestFile{}
		for _, name := range slices.Sorted(maps.Keys(tests)) {
			tf.Tests = append(tf.Tests, MTETest{ID: name, Name: name})
		}
		out.TestFiles = map[string]*MTETestFile{"": tf}
	}
	return out, nil
}

func mteStatus(m Mutation) (string, string) {
	switch {
	case m.Accepted != "":
		return "Ignored", "accepted: " + m.Accepted
	case m.Status == string(run.Equivalent):
		return "Ignored", "equivalent: " + m.Reason
	}
	switch run.Status(m.Status) {
	case run.Killed:
		return "Killed", ""
	case run.Lived:
		return "Survived", m.Reason
	case run.NotCovered:
		return "NoCoverage", ""
	case run.NotViable:
		return "CompileError", ""
	case run.TimedOut:
		return "Timeout", ""
	case run.Pending:
		return "Pending", ""
	}
	return "RuntimeError", m.Status
}

// span は、line:column から字句 token の長さだけの範囲を返す。
func span(line, column int, token string) MTELocation {
	return MTELocation{Start: MTEPosition{line, column}, End: MTEPosition{line, column + max(1, len(token))}}
}

// viewerScript は、HTML の報告が読み込む閲覧画面のスクリプトである。報告のスキーマと同じ版に固定する。
const viewerScript = "https://cdn.jsdelivr.net/npm/mutation-testing-elements@3.9.0/dist/mutation-test-elements.js"

// WriteHTML は、報告を埋め込んだ 1 つの HTML を書く。閲覧画面のスクリプトは同梱せず、固定した版を読み込む。
// encoding/json は < と > をエスケープするので、埋め込んだソースの "</script>" で script 要素が閉じない。
func WriteHTML(w io.Writer, out *MTEReport) error {
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>gamut mutation report</title>
<script src="%s"></script>
</head>
<body>
<mutation-test-report-app title-postfix="gamut"></mutation-test-report-app>
<script>
document.querySelector("mutation-test-report-app").report = %s;
</script>
</body>
</html>
`, viewerScript, data)
	return err
}
