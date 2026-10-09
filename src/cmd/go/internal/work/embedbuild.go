// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package work

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"cmd/go/internal/base"
	"cmd/go/internal/cfg"
	"cmd/go/internal/load"
)

// embedBuildOutsideModule is set by go install pkg@version. The inner build
// then installs its package at the same version, so it sees the same module
// graph and version stamp as the outer one.
var embedBuildOutsideModule bool

// embedBuildStackEnv records the builds in progress, to reject a package
// that embeds a build of itself for the same target.
const embedBuildStackEnv = "GOEMBEDBUILDSTACK"

// embedBuildActor runs a nested go command for a //go:embedbuild directive.
type embedBuildActor struct {
	pkg *load.Package
	eb  load.EmbedBuild
}

func (b *Builder) embedBuildAction(p *load.Package, eb load.EmbedBuild) *Action {
	return b.cacheAction("embed build "+eb.Name, p, func() *Action {
		a := &Action{
			Mode:    "embed build",
			Package: p,
			Actor:   &embedBuildActor{pkg: p, eb: eb},
			Objdir:  b.NewObjdir(),
		}
		a.Target = filepath.Join(a.Objdir, filepath.Base(eb.Name))
		return a
	})
}

func (e *embedBuildActor) Act(b *Builder, ctx context.Context, a *Action) error {
	defer b.flushOutput(a)
	sh := b.Shell(a)
	if err := sh.Mkdir(a.Objdir); err != nil {
		return err
	}

	key := e.eb.Pkg + " " + strings.Join(e.eb.Env, " ")
	stack := os.Getenv(embedBuildStackEnv)
	if slices.Contains(strings.Split(stack, "\n"), key) {
		return fmt.Errorf("//go:embedbuild cycle: %s", key)
	}

	// The outer target's cgo and architecture settings do not carry over.
	env := append([]string{embedBuildStackEnv + "=" + strings.TrimPrefix(stack+"\n"+key, "\n"), "GOFLAGS=" + embedBuildFlags()}, "CGO_ENABLED=0")
	for _, k := range slices.Sorted(maps.Keys(load.EmbedBuildEnv)) {
		if k != "GOOS" && k != "GOARCH" && k != "CGO_ENABLED" {
			env = append(env, k+"=")
		}
	}
	env = append(env, e.eb.Env...)
	gocmd := filepath.Join(cfg.GOROOTbin, "go"+cfg.ToolExeSuffix())
	var args []string
	if e.eb.Tags != "" {
		args = append(args, "-tags="+e.eb.Tags)
	}
	if e.eb.Strip {
		args = append(args, "-ldflags=-s -w")
	}
	raw := a.Target
	if e.eb.Gzip {
		raw = filepath.Join(a.Objdir, "embedbuild.out")
	}

	if embedBuildOutsideModule && e.pkg.Module != nil && e.pkg.Module.Version != "" {
		// go install refuses a cross-compiled binary when GOBIN is set, so
		// give it a scratch GOPATH and keep the shared module cache.
		gopath := filepath.Join(a.Objdir, "gopath")
		// An empty GOBIN falls back to the go env file, so hand the inner
		// command a copy of that file without it.
		goenv, err := embedBuildGoenv(a.Objdir)
		if err != nil {
			return err
		}
		env = append(env, "GOBIN=", "GOENV="+goenv, "GOPATH="+gopath, "GOMODCACHE="+cfg.GOMODCACHE)
		if err := sh.run(".", e.pkg.ImportPath, env, gocmd, "install", args, e.eb.Pkg+"@"+e.pkg.Module.Version); err != nil {
			return err
		}
		if !cfg.BuildN {
			bins, _ := filepath.Glob(filepath.Join(gopath, "bin", "*", "*"))
			if len(bins) != 1 {
				return fmt.Errorf("%s: //go:embedbuild: expected one binary, found %d", e.pkg.ImportPath, len(bins))
			}
			if err := sh.moveOrCopyFile(raw, bins[0], 0o644, false); err != nil {
				return err
			}
		}
	} else if err := sh.run(base.Cwd(), e.pkg.ImportPath, env, gocmd, "build", args, "-o", raw, e.eb.Pkg); err != nil {
		return err
	}

	if e.eb.Gzip && !cfg.BuildN {
		if err := gzipFile(raw, a.Target); err != nil {
			return err
		}
	}
	a.built = a.Target
	return nil
}

// embedBuildFlags returns the GOFLAGS for the inner build: the caller's
// GOFLAGS plus the build flags that change what a build produces.
func embedBuildFlags() string {
	flags := strings.Fields(os.Getenv("GOFLAGS"))
	if cfg.BuildA {
		flags = append(flags, "-a")
	}
	if cfg.BuildTrimpath {
		flags = append(flags, "-trimpath")
	}
	if cfg.BuildModExplicit {
		flags = append(flags, "-mod="+cfg.BuildMod)
	}
	flags = append(flags, "-buildvcs="+cfg.BuildBuildvcs)
	return strings.Join(flags, " ")
}

// gzipFile compresses src into dst with no name or time in the header, so
// equal inputs give equal bytes.
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		out.Close()
		return err
	}
	if _, err := io.Copy(zw, in); err != nil {
		out.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// embedBuildGoenv writes the go env file, less any GOBIN line, into dir and
// returns its path.
func embedBuildGoenv(dir string) (string, error) {
	var keep []string
	if file, _, err := cfg.EnvFile(); err == nil {
		if data, err := os.ReadFile(file); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "GOBIN=") {
					keep = append(keep, line)
				}
			}
		}
	}
	name := filepath.Join(dir, "goenv")
	if cfg.BuildN {
		return name, nil
	}
	return name, os.WriteFile(name, []byte(strings.Join(keep, "\n")), 0o666)
}
