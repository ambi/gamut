package report

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ambi/gamut/internal/run"
)

// Merge combines the reports of the shards into one. For the same mutant, the verdict that is not PENDING wins.
// Counts are recomputed, and so is the kill matrix. Values fixed by enumeration (the arid count, disabled mutants) and by coverage records
// are the same in every shard, so they come from the first report.
func Merge(reps []*Report) (*Report, error) {
	if len(reps) == 0 {
		return nil, fmt.Errorf("no reports to merge")
	}
	out := *reps[0]
	out.Files = nil
	byPath := map[string]*File{}
	var paths []string
	matrix := false
	for _, r := range reps {
		if r.GoModule != reps[0].GoModule {
			return nil, fmt.Errorf("the reports are of different modules: %s and %s", reps[0].GoModule, r.GoModule)
		}
		out.ElapsedTime = max(out.ElapsedTime, r.ElapsedTime)
		matrix = matrix || r.KillMatrix != nil
		for _, f := range r.Files {
			mf := byPath[f.Path]
			if mf == nil {
				mf = &File{FileName: f.FileName, Path: f.Path}
				byPath[f.Path] = mf
				paths = append(paths, f.Path)
			}
			for _, m := range f.Mutations {
				i := slices.IndexFunc(mf.Mutations, func(o Mutation) bool { return o.ID == m.ID })
				switch {
				case i < 0:
					mf.Mutations = append(mf.Mutations, m)
				case mf.Mutations[i].Status == string(run.Pending):
					mf.Mutations[i] = m
				}
			}
		}
		for _, name := range r.UnrepeatableTests {
			if !slices.Contains(out.UnrepeatableTests, name) {
				out.UnrepeatableTests = append(out.UnrepeatableTests, name)
			}
		}
		for _, s := range r.StaleAccepted {
			if !slices.ContainsFunc(out.StaleAccepted, func(o StaleAcceptance) bool { return o.ID == s.ID }) {
				out.StaleAccepted = append(out.StaleAccepted, s)
			}
		}
	}
	slices.Sort(paths)
	for _, p := range paths {
		f := byPath[p]
		slices.SortStableFunc(f.Mutations, func(a, b Mutation) int {
			if a.Line != b.Line {
				return a.Line - b.Line
			}
			return a.Column - b.Column
		})
		out.Files = append(out.Files, *f)
	}
	slices.Sort(out.UnrepeatableTests)
	slices.SortFunc(out.StaleAccepted, func(a, b StaleAcceptance) int { return strings.Compare(a.ID, b.ID) })
	out.count()
	out.KillMatrix = nil
	if matrix {
		out.ComputeKillMatrix()
	}
	return &out, nil
}
