package main

import "runtime/debug"

// version returns the version of the running gamut.
func version() string {
	info, _ := debug.ReadBuildInfo()
	return versionOf(info)
}

// versionOf returns the version from the build info: the version given to go install (such as v0.1.0),
// or for a local build, devel with the commit and whether there are local changes when known.
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
