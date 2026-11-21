package main

import (
	"io"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionOf(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"installed with go install at a tag", &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, "v0.1.0"},
		{"built in a clean checkout", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "e1fd6fd0123456789abcdef"}, {Key: "vcs.modified", Value: "false"}}}, "devel (e1fd6fd01234)"},
		{"built with local changes", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "e1fd6fd0123456789abcdef"}, {Key: "vcs.modified", Value: "true"}}}, "devel (e1fd6fd01234, modified)"},
		{"no version and no VCS information", &debug.BuildInfo{}, "devel"},
		{"no build information", nil, "devel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionOf(tt.info); got != tt.want {
				t.Errorf("versionOf = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionFlag(t *testing.T) {
	var out strings.Builder
	if code := realMain([]string{"-version"}, &out, io.Discard); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(out.String(), "gamut ") || strings.Count(out.String(), "\n") != 1 {
		t.Errorf("-version printed %q, want one line starting with \"gamut \"", out.String())
	}
}
