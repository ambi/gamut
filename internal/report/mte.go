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
	"strings"

	"github.com/ambi/gamut/internal/run"
)

// MTEReport は、mutation-testing-elements のスキーマ 2（mutation-testing-report-schema）の報告である。gamut の機械向けの報告はこれだけである。
// このスキーマを読む HTML の閲覧画面やダッシュボードで、gamut の判定を表示できる。
// gamut 固有の情報は、スキーマが禁じない追加のフィールド gamut にまとめる。スキーマが将来フィールドを足しても衝突しないためである。
// 外向きの形なので、内部の Report の形とは切り離し、ここで写す。
type MTEReport struct {
	SchemaVersion string                  `json:"schemaVersion"`
	Thresholds    MTEThresholds           `json:"thresholds"`
	ProjectRoot   string                  `json:"projectRoot,omitempty"`
	Files         map[string]*MTEFile     `json:"files"`
	TestFiles     map[string]*MTETestFile `json:"testFiles,omitempty"`
	Framework     *MTEFramework           `json:"framework,omitempty"`
	Gamut         *MTEGamut               `json:"gamut,omitempty"`
}

// MTEGamut は、報告の全体についての gamut 固有の情報である。
type MTEGamut struct {
	GoModule string `json:"goModule"`
	// Package は、1 パッケージならその import パス、複数なら実行時に指定したパターンである。
	Package  string   `json:"package"`
	Packages []string `json:"packages,omitempty"`
	// MutationScore は、検出した変異体の、検出したものと生き残り（Survived、NoCoverage）の和に対する百分率である。閾値はこれに当てる。
	MutationScore float64 `json:"mutationScore"`
	// TestEfficacy は、到達した変異体のうち検出したものの百分率、MutationsCoverage は、判定した変異体のうち到達したものの百分率である。
	TestEfficacy      float64 `json:"testEfficacy"`
	MutationsCoverage float64 `json:"mutationsCoverage"`
	// ThresholdBreak は、点数がこれ未満なら品質の条件を満たさない閾値である。0 なら使わない。
	ThresholdBreak float64   `json:"thresholdBreak,omitempty"`
	ElapsedTime    float64   `json:"elapsedTime"`
	Counts         MTECounts `json:"counts"`
	// StaleAccepted は、もう要らない受理である。
	StaleAccepted []MTEStaleAcceptance `json:"staleAccepted,omitempty"`
	// UnrepeatableTests は、同じプロセスで 2 回目に失敗したテストである。それが到達する変異体は、プロセスを使い回さずに判定した。
	UnrepeatableTests []string `json:"unrepeatableTests,omitempty"`
	// MaxDepthExcluded は、-max-depth を超えてだけ変異箇所に到達したので到達から除いたテストと、到達（テストと箇所の組）の数である。
	MaxDepthExcluded *MTEMaxDepthExcluded `json:"maxDepthExcluded,omitempty"`
	SkippedPackages  []MTESkippedPackage  `json:"skippedPackages,omitempty"`
	KillMatrix       *MTEKillMatrix       `json:"killMatrix,omitempty"`
}

// MTECounts は、状態ごとの変異体の数である。Total と点数は、Pending と Ignored（受理、等価、無効化コメント）を含めない。
type MTECounts struct {
	Total        int `json:"total"`
	Killed       int `json:"killed"`
	Survived     int `json:"survived"`
	NoCoverage   int `json:"noCoverage"`
	CompileError int `json:"compileError"`
	Timeout      int `json:"timeout"`
	Pending      int `json:"pending,omitempty"`
	// Arid は、テストが観測しないことの多いコード（ログ出力など）の中なので作らなかった変異体の数である。
	Arid       int `json:"arid,omitempty"`
	Ignored    int `json:"ignored,omitempty"`
	Equivalent int `json:"equivalent,omitempty"`
	Accepted   int `json:"accepted,omitempty"`
}

// MTEStaleAcceptance は、もう要らない受理である。Why は detected（テストが検出するようになった）か missing（変異体がなくなった）である。
type MTEStaleAcceptance struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Why    string `json:"why"`
}

// MTEMaxDepthExcluded は、-max-depth で到達から除いたテストと到達の数である。
type MTEMaxDepthExcluded struct {
	Tests   int `json:"tests"`
	Reaches int `json:"reaches"`
}

// MTESkippedPackage は、判定できずに飛ばしたパッケージと理由である。
type MTESkippedPackage struct {
	Package string `json:"package"`
	Reason  string `json:"reason"`
}

// MTEKillMatrix は、-kill-matrix のときの、テストの見直しの候補である。
type MTEKillMatrix struct {
	Mutants               int              `json:"mutants"`
	Excluded              int              `json:"excluded"`
	TestsDetectingNothing []string         `json:"testsDetectingNothing"`
	SubsumedTests         []MTESubsumption `json:"subsumedTests"`
	DuplicateTests        [][]string       `json:"duplicateTests"`
}

// MTESubsumption は、Test が検出する変異体をすべて By のどれかも検出することである。
type MTESubsumption struct {
	Test string   `json:"test"`
	By   []string `json:"by"`
}

// MTEMutantGamut は、変異体についての gamut 固有の情報である。値のないフィールドは省く。
type MTEMutantGamut struct {
	// Original は置き換える前の字句である。
	Original string `json:"original,omitempty"`
	// Mode は、schemata に埋め込んで判定したか（schemata）、個別にビルドしたか（standalone）である。
	Mode   string `json:"mode,omitempty"`
	Cached bool   `json:"cached,omitempty"`
	// Critical は、重要領域の変異体かである。
	Critical bool `json:"critical,omitempty"`
	// PreviouslyKilledBy は、前回の検出テストが消えたので再検査し、今回は検出されなかったときの、前回の検出テストである。
	PreviouslyKilledBy string `json:"previouslyKilledBy,omitempty"`
	// AcceptedStatus は、受理した変異体（status は Ignored）の判定した状態である。
	AcceptedStatus string `json:"acceptedStatus,omitempty"`
}

// MTEThresholds は閾値（整数の百分率）である。
type MTEThresholds struct {
	High int `json:"high"`
	Low  int `json:"low"`
}

// MTEFramework は報告を作ったツールである。
type MTEFramework struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
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
	ID           string          `json:"id"`
	MutatorName  string          `json:"mutatorName"`
	Replacement  string          `json:"replacement,omitempty"`
	Location     MTELocation     `json:"location"`
	Status       string          `json:"status"`
	StatusReason string          `json:"statusReason,omitempty"`
	KilledBy     []string        `json:"killedBy,omitempty"`
	CoveredBy    []string        `json:"coveredBy,omitempty"`
	Gamut        *MTEMutantGamut `json:"gamut,omitempty"`
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
	out := &MTEReport{SchemaVersion: "2", ProjectRoot: moduleDir, Files: map[string]*MTEFile{}, Framework: &MTEFramework{Name: "gamut", Version: r.Version}}
	t := Thresholds{High: 80, Low: 60}
	if r.Thresholds != nil {
		t = *r.Thresholds
	}
	out.Thresholds = MTEThresholds{High: int(math.Round(t.High)), Low: int(math.Round(t.Low))}
	out.Gamut = r.mteGamut(t.Break)
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
				Gamut: &MTEMutantGamut{Original: m.Original, Mode: m.Mode, Cached: m.Cached, Critical: m.Critical, PreviouslyKilledBy: m.PreviouslyKilledBy},
			}
			if m.Accepted != "" {
				mm.Gamut.AcceptedStatus, _ = mteStatus(Mutation{Status: m.Status, Reason: m.Reason})
			}
			switch {
			case len(m.Killers) > 0:
				mm.KilledBy = m.Killers
			case m.KilledBy != "":
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
			Gamut: &MTEMutantGamut{Original: ig.Original},
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

// span は、line:column から字句 token の終わりまでの範囲を返す。列はバイトで数える。
// 字句が複数行にわたるなら（分岐の本体など）、終わりは最後の改行の後ろの行と列にする。空の字句も 1 字の範囲にする。
func span(line, column int, token string) MTELocation {
	start := MTEPosition{line, column}
	if n := strings.Count(token, "\n"); n > 0 {
		return MTELocation{Start: start, End: MTEPosition{line + n, len(token) - strings.LastIndex(token, "\n")}}
	}
	return MTELocation{Start: start, End: MTEPosition{line, column + max(1, len(token))}}
}

// mteGamut は、報告の全体についての gamut 固有の情報を写す。
func (r *Report) mteGamut(thresholdBreak float64) *MTEGamut {
	g := &MTEGamut{
		GoModule: r.GoModule, Package: r.Package, Packages: r.Packages,
		MutationScore: r.MutationScore, TestEfficacy: r.TestEfficacy, MutationsCoverage: r.MutationsCoverage,
		ThresholdBreak: thresholdBreak, ElapsedTime: r.ElapsedTime, UnrepeatableTests: r.UnrepeatableTests,
		Counts: MTECounts{
			Total: r.MutantsTotal, Killed: r.MutantsKilled, Survived: r.MutantsLived, NoCoverage: r.MutantsNotCovered,
			CompileError: r.MutantsNotViable, Timeout: r.MutantsTimedOut, Pending: r.MutantsPending, Arid: r.MutantsArid,
			Ignored: r.MutantsIgnored, Equivalent: r.MutantsEquivalent, Accepted: r.MutantsAccepted,
		},
	}
	for _, s := range r.StaleAccepted {
		g.StaleAccepted = append(g.StaleAccepted, MTEStaleAcceptance(s))
	}
	if r.MaxDepthExcludedTests > 0 {
		g.MaxDepthExcluded = &MTEMaxDepthExcluded{Tests: r.MaxDepthExcludedTests, Reaches: r.MaxDepthExcludedReaches}
	}
	for _, s := range r.SkippedPackages {
		g.SkippedPackages = append(g.SkippedPackages, MTESkippedPackage(s))
	}
	if k := r.KillMatrix; k != nil {
		g.KillMatrix = &MTEKillMatrix{Mutants: k.Mutants, Excluded: k.Excluded, TestsDetectingNothing: k.NoKill, DuplicateTests: k.Duplicates}
		for _, s := range k.Subsumed {
			g.KillMatrix.SubsumedTests = append(g.KillMatrix.SubsumedTests, MTESubsumption(s))
		}
	}
	return g
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
