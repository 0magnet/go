// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package load

import (
	"fmt"
	"go/build"
	"path"
	"strings"
)

// An EmbedBuild is a //go:embedbuild directive. It asks the go command to
// build Pkg for the target set in Env and to embed the result as Name, a file
// under one of the package's //go:embed patterns.
//
//	//go:embedbuild [-gzip] [-strip] [-tags=list] name [KEY=value...] pkg
type EmbedBuild struct {
	Name    string   // embedded file name, relative to the package directory
	Pattern string   // the //go:embed pattern that Name falls under
	Env     []string // KEY=value settings for the inner build
	Pkg     string   // package to build
	Gzip    bool     // compress the result with gzip
	Strip   bool     // link with -s -w
	Tags    string   // comma separated build tags for the inner build
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
			default:
				return nil, fmt.Errorf("%v: //go:embedbuild: unknown flag %s", d.Pos, flag)
			}
			f = f[1:]
		}
		if len(f) < 2 {
			return nil, fmt.Errorf("%v: usage: //go:embedbuild [-gzip] [-strip] [-tags=list] name [KEY=value...] pkg", d.Pos)
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
		ebs = append(ebs, eb)
	}
	return ebs, nil
}
