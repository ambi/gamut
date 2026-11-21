package main

import "runtime/debug"

// version は、実行中の gamut の版を返す。
func version() string {
	info, _ := debug.ReadBuildInfo()
	return versionOf(info)
}

// versionOf は、ビルド情報から版を返す。go install で版を指定して入れたならその版（v0.1.0 など）、
// 手元でビルドしたなら devel と、分かればコミットと手元の変更の有無を返す。
func versionOf(info *debug.BuildInfo) string {
	if info == nil {
		return "devel"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if revision == "" {
		return "devel"
	}
	out := "devel (" + revision[:min(12, len(revision))]
	if modified == "true" {
		out += ", modified"
	}
	return out + ")"
}
