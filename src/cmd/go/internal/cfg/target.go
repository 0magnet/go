// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cfg

import (
	"go/build"
	"internal/buildcfg"
	"strings"
)

// SwitchTarget makes goos/goarch the go command's target until restore is
// called. Cgo, instrumentation and the outer build tags are off, tags are
// the build tags, and env may set the GOARCH-specific variable. It is used
// to load the packages of a //go:embedbuild directive.
func SwitchTarget(goos, goarch string, tags, env []string) (restore func()) {
	type archVar struct {
		key     string
		val     *string
		changed *bool
		def     string
	}
	arch := []archVar{
		{"GO386", &GO386, &go386Changed, buildcfg.DefaultGO386},
		{"GOAMD64", &GOAMD64, &goAMD64Changed, buildcfg.DefaultGOAMD64},
		{"GOARM", &GOARM, &goARMChanged, buildcfg.DefaultGOARM},
		{"GOARM64", &GOARM64, &goARM64Changed, buildcfg.DefaultGOARM64},
		{"GOMIPS", &GOMIPS, &goMIPSChanged, buildcfg.DefaultGOMIPS},
		{"GOMIPS64", &GOMIPS64, &goMIPS64Changed, buildcfg.DefaultGOMIPS64},
		{"GOPPC64", &GOPPC64, &goPPC64Changed, buildcfg.DefaultGOPPC64},
		{"GORISCV64", &GORISCV64, &goRISCV64Changed, buildcfg.DefaultGORISCV64},
		{"GOWASM", &GOWASM, &goWASMChanged, ""},
	}
	savedArch := make([]string, len(arch))
	savedChanged := make([]bool, len(arch))
	for i, v := range arch {
		savedArch[i], savedChanged[i] = *v.val, *v.changed
	}
	hostArch := Goarch
	saved := struct {
		goos, goarch, exe, buildmode    string
		ctxt                            build.Context
		exp                             *buildcfg.ExperimentFlags
		expErr                          error
		raw, clean                      string
		race, msan, asan, cover, shared bool
	}{Goos, Goarch, ExeSuffix, BuildBuildmode, BuildContext, Experiment, ExperimentErr,
		RawGOEXPERIMENT, CleanGOEXPERIMENT, BuildRace, BuildMSan, BuildASan, BuildCover, BuildLinkshared}

	Goos, Goarch = goos, goarch
	ExeSuffix = exeSuffix()
	BuildBuildmode = "default"
	BuildRace, BuildMSan, BuildASan, BuildCover, BuildLinkshared = false, false, false, false, false
	for _, v := range arch {
		*v.val, *v.changed = v.def, false
		for _, kv := range env {
			if k, val, ok := strings.Cut(kv, "="); ok && k == v.key {
				*v.val, *v.changed = val, true
			}
		}
	}
	ctxt := defaultContext()
	ctxt.GOROOT = BuildContext.GOROOT
	ctxt.CgoEnabled = false
	ctxt.BuildTags = tags
	// build.Default's tool tags include feature levels of the host
	// architecture, such as amd64.v1, which do not apply to goarch.
	var toolTags []string
	for _, t := range ctxt.ToolTags {
		if !strings.HasPrefix(t, hostArch+".") || hostArch == goarch {
			toolTags = append(toolTags, t)
		}
	}
	ctxt.ToolTags = toolTags
	BuildContext = ctxt
	RawGOEXPERIMENT = envOr("GOEXPERIMENT", buildcfg.DefaultGOEXPERIMENT)
	CleanGOEXPERIMENT = RawGOEXPERIMENT
	computeExperiment()

	return func() {
		Goos, Goarch, ExeSuffix, BuildBuildmode = saved.goos, saved.goarch, saved.exe, saved.buildmode
		BuildContext = saved.ctxt
		Experiment, ExperimentErr = saved.exp, saved.expErr
		RawGOEXPERIMENT, CleanGOEXPERIMENT = saved.raw, saved.clean
		BuildRace, BuildMSan, BuildASan, BuildCover, BuildLinkshared = saved.race, saved.msan, saved.asan, saved.cover, saved.shared
		for i, v := range arch {
			*v.val, *v.changed = savedArch[i], savedChanged[i]
		}
	}
}
