// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package load

import (
	"context"
	"errors"
	"fmt"
	"go/build"
	"path"
	"slices"
	"strings"

	"cmd/go/internal/cfg"
	"cmd/go/internal/modload"
	"cmd/internal/par"
)

// An EmbedBuild is a //go:embedbuild directive. It asks the go command to
// build Pkg for the target set in Env and to embed the result as Name, a file
// under one of the package's //go:embed patterns.
//
//	//go:embedbuild [-gzip] [-strip] [-tags=list] [-fallback] name [KEY=value...] pkg
//
// A file named Name on disk must be marked -fallback. It is what a toolchain
// without the directive embeds, and what is embedded when the directive is
// inactive.
type EmbedBuild struct {
	Name     string   // embedded file name, relative to the package directory
	Pattern  string   // the //go:embed pattern that Name falls under
	Env      []string `json:",omitempty"` // KEY=value settings for the inner build
	Pkg      string   // package to build
	Gzip     bool     `json:",omitempty"` // compress the result with gzip
	Strip    bool     `json:",omitempty"` // link with -s -w
	Tags     string   `json:",omitempty"` // comma separated build tags for the inner build
	Fallback bool     `json:",omitempty"` // a file named Name exists on disk
	Inactive string   `json:",omitempty"` // why the directive is not used, if it is not
	Main     *Package `json:"-"`          // Pkg loaded for its target, when the go command builds
}

// EmbedBuildEnv lists the variables a //go:embedbuild directive may set.
var EmbedBuildEnv = map[string]bool{
	"GOOS": true, "GOARCH": true, "CGO_ENABLED": true,
	"GO386": true, "GOAMD64": true, "GOARM": true, "GOARM64": true,
	"GOMIPS": true, "GOMIPS64": true, "GOPPC64": true, "GORISCV64": true, "GOWASM": true,
}

// resolveEmbedBuilds parses the //go:embedbuild directives in directives.
// embed maps each //go:embed pattern to the files it matched.
func resolveEmbedBuilds(directives []build.Directive, embed map[string][]string) ([]EmbedBuild, error) {
	var ebs []EmbedBuild
	for _, d := range directives {
		args, ok := strings.CutPrefix(d.Text, "//go:embedbuild")
		if !ok || (args != "" && args[0] != ' ' && args[0] != '\t') {
			continue
		}
		f := strings.Fields(args)
		var eb EmbedBuild
		for len(f) > 0 && strings.HasPrefix(f[0], "-") {
			switch flag := f[0]; {
			case flag == "-gzip":
				eb.Gzip = true
			case flag == "-strip":
				eb.Strip = true
			case strings.HasPrefix(flag, "-tags="):
				eb.Tags = flag[len("-tags="):]
			case flag == "-fallback":
				eb.Fallback = true
			default:
				return nil, fmt.Errorf("%v: //go:embedbuild: unknown flag %s", d.Pos, flag)
			}
			f = f[1:]
		}
		if len(f) < 2 {
			return nil, fmt.Errorf("%v: usage: //go:embedbuild [-gzip] [-strip] [-tags=list] [-fallback] name [KEY=value...] pkg", d.Pos)
		}
		eb.Name, eb.Pkg = f[0], f[len(f)-1]
		for _, kv := range f[1 : len(f)-1] {
			k, _, ok := strings.Cut(kv, "=")
			if !ok || !EmbedBuildEnv[k] {
				return nil, fmt.Errorf("%v: //go:embedbuild: invalid setting %q", d.Pos, kv)
			}
			eb.Env = append(eb.Env, kv)
		}
		if eb.Name != path.Clean(eb.Name) || path.IsAbs(eb.Name) || strings.HasPrefix(eb.Name, "../") || !validEmbedPattern(eb.Name) {
			return nil, fmt.Errorf("%v: //go:embedbuild: invalid name %q", d.Pos, eb.Name)
		}
		for pattern := range embed {
			p := strings.TrimPrefix(pattern, "all:")
			if p == eb.Name || strings.HasPrefix(eb.Name, p+"/") {
				eb.Pattern = pattern
				break
			}
		}
		if eb.Pattern == "" {
			return nil, fmt.Errorf("%v: //go:embedbuild: %s is not under any //go:embed pattern", d.Pos, eb.Name)
		}
		switch onDisk := slices.Contains(embed[eb.Pattern], eb.Name); {
		case onDisk && !eb.Fallback:
			return nil, fmt.Errorf("%v: //go:embedbuild: %s exists; mark it -fallback to embed it when the directive is not used, or remove it", d.Pos, eb.Name)
		case !onDisk && eb.Fallback:
			return nil, fmt.Errorf("%v: //go:embedbuild: -fallback, but %s does not exist", d.Pos, eb.Name)
		}
		ebs = append(ebs, eb)
	}
	return ebs, nil
}

// A Target is the target of the packages loaded for a //go:embedbuild
// directive, when it differs from the go command's own.
type Target struct {
	GOOS, GOARCH string
	ArchEnv      string   // the GOARCH-specific variable as KEY=value, or ""
	Env          []string // environment for the compiler, assembler and linker
}

// EmbedBuildsEnabled makes PackagesAndErrors load the package of each
// //go:embedbuild directive, for the commands that build.
var EmbedBuildsEnabled bool

// embedBuildStack holds the directives being loaded, to reject a cycle.
var embedBuildStack []string

func loadEmbedBuilds(ld *modload.Loader, ctx context.Context, opts PackageOpts, pkgs []*Package) {
	all := PackageList(pkgs)
	// A directive is used only in the modules being built, so a dependency
	// never adds a build to its importers.
	building := map[string]bool{}
	for _, p := range all {
		if p.Internal.CmdlinePkg && p.Module != nil {
			building[p.Module.Path] = true
		}
	}
	for _, p := range all {
		for i := range p.EmbedBuilds {
			eb := &p.EmbedBuilds[i]
			if eb.Main != nil || eb.Inactive != "" || p.Error != nil {
				continue
			}
			switch {
			case cfg.BuildEmbedBuild == "off":
				eb.Inactive = "-embedbuild=off"
			case p.Module != nil && !p.Module.Main && !building[p.Module.Path]:
				eb.Inactive = "not in a module being built"
			}
			if eb.Inactive != "" || !EmbedBuildsEnabled {
				continue
			}
			if err := loadEmbedBuild(ld, ctx, opts, eb); err != nil {
				var ebe *EmbedBuildError
				if !errors.As(err, &ebe) {
					ebe = &EmbedBuildError{Name: eb.Name, Err: err}
				}
				p.Error = &PackageError{Err: ebe}
				p.Incomplete = true
			}
		}
	}
}

func loadEmbedBuild(ld *modload.Loader, ctx context.Context, opts PackageOpts, eb *EmbedBuild) error {
	goos, goarch := cfg.Goos, cfg.Goarch
	for _, kv := range eb.Env {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "GOOS":
			goos = v
		case "GOARCH":
			goarch = v
		case "CGO_ENABLED":
			if v != "0" {
				return fmt.Errorf("cgo is not supported")
			}
		}
	}
	key := eb.Pkg + " " + goos + "/" + goarch
	if slices.Contains(embedBuildStack, key) {
		return fmt.Errorf("cycle: %s", strings.Join(append(embedBuildStack, key), " -> "))
	}
	embedBuildStack = append(embedBuildStack, key)
	defer func() { embedBuildStack = embedBuildStack[:len(embedBuildStack)-1] }()

	var tags []string
	if eb.Tags != "" {
		tags = strings.Split(eb.Tags, ",")
	}
	defer cfg.SwitchTarget(goos, goarch, tags, eb.Env)()
	t := &Target{GOOS: goos, GOARCH: goarch}
	t.Env = []string{"GOOS=" + goos, "GOARCH=" + goarch, "CGO_ENABLED=0"}
	if k, v, _ := cfg.GetArchEnv(); k != "" {
		t.ArchEnv = k + "=" + v
		t.Env = append(t.Env, t.ArchEnv)
	}

	// Packages loaded for another target are different packages.
	savedPkgs, savedData, savedResolved := packageCache, packageDataCache, resolvedImportCache
	packageCache = map[string]*Package{}
	packageDataCache = new(par.ErrCache[string, *build.Package])
	resolvedImportCache = new(par.Cache[importSpec, resolvedImport])
	defer func() { packageCache, packageDataCache, resolvedImportCache = savedPkgs, savedData, savedResolved }()
	defer ld.SavePackageLoader()()

	pkgs := PackagesAndErrors(ld, ctx, PackageOpts{AutoVCS: opts.AutoVCS}, []string{eb.Pkg})
	if len(pkgs) != 1 {
		return fmt.Errorf("%s matched %d packages", eb.Pkg, len(pkgs))
	}
	main := pkgs[0]
	for _, p := range PackageList(pkgs) {
		if p.Error != nil {
			return p.Error
		}
		p.Internal.Target = t
	}
	if main.Name != "main" {
		return fmt.Errorf("%s is not a main package", eb.Pkg)
	}
	main.Internal.CmdlinePkg = false
	main.Internal.Gcflags, main.Internal.Asmflags, main.Internal.Ldflags = nil, nil, nil
	if eb.Strip {
		main.Internal.Ldflags = []string{"-s", "-w"}
	}
	eb.Main = main
	return nil
}

// An EmbedBuildError is an error loading the package of a //go:embedbuild
// directive. A nested directive's error is reported once, unwrapped.
type EmbedBuildError struct {
	Name string
	Err  error
}

func (e *EmbedBuildError) Error() string { return "//go:embedbuild " + e.Name + ": " + e.Err.Error() }
func (e *EmbedBuildError) Unwrap() error { return e.Err }
