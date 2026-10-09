// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package work

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"cmd/go/internal/cache"
	"cmd/go/internal/cfg"
	"cmd/go/internal/load"
)

// The helpers below return the target of a package loaded for a
// //go:embedbuild directive, or the go command's own target. Such packages
// are built without cgo, instrumentation or the outer build mode.

func goosOf(p *load.Package) string {
	if p != nil && p.Internal.Target != nil {
		return p.Internal.Target.GOOS
	}
	return cfg.Goos
}

func goarchOf(p *load.Package) string {
	if p != nil && p.Internal.Target != nil {
		return p.Internal.Target.GOARCH
	}
	return cfg.Goarch
}

// archEnvOf returns the GOARCH-specific variable, such as GOAMD64.
func archEnvOf(p *load.Package) (key, val string) {
	if p != nil && p.Internal.Target != nil {
		key, val, _ = strings.Cut(p.Internal.Target.ArchEnv, "=")
		return key, val
	}
	key, val, _ = cfg.GetArchEnv()
	return key, val
}

// archVal returns def, the go command's value of the GOARCH-specific
// variable, or the target's value for a //go:embedbuild package.
func archVal(p *load.Package, def string) string {
	if p != nil && p.Internal.Target != nil {
		_, val := archEnvOf(p)
		return val
	}
	return def
}

// toolEnv returns the environment overrides for the compiler, assembler
// and linker.
func toolEnv(p *load.Package) []string {
	if p != nil && p.Internal.Target != nil {
		return slices.Clip(p.Internal.Target.Env)
	}
	return cfgChangedEnv
}

func buildmodeOf(p *load.Package) string {
	if p != nil && p.Internal.Target != nil {
		return "exe"
	}
	return cfg.BuildBuildmode
}

func ldBuildmodeOf(p *load.Package) string {
	if p != nil && p.Internal.Target != nil {
		return "exe"
	}
	return ldBuildmode
}

func installSuffix(p *load.Package) string {
	if p != nil && p.Internal.Target != nil {
		return ""
	}
	return cfg.BuildContext.InstallSuffix
}

func forcedGcflagsOf(p *load.Package) []string {
	if p != nil && p.Internal.Target != nil {
		return nil
	}
	return forcedGcflags
}

func forcedAsmflagsOf(p *load.Package) []string {
	if p != nil && p.Internal.Target != nil {
		return nil
	}
	return forcedAsmflags
}

func forcedLdflagsOf(p *load.Package) []string {
	if p != nil && p.Internal.Target != nil {
		return nil
	}
	return forcedLdflags
}

// embedBuildActor provides the file a //go:embedbuild directive embeds:
// the linked executable, gzipped if asked.
type embedBuildActor struct {
	eb load.EmbedBuild
}

// embedBuildAction returns the action that provides eb's file to p's
// compile, or nil when the directive's package was not loaded.
func (b *Builder) embedBuildAction(p *load.Package, eb load.EmbedBuild) *Action {
	if eb.Main == nil {
		return nil
	}
	return b.cacheAction("embed build "+eb.Name, p, func() *Action {
		link := b.LinkAction(nil, ModeBuild, ModeBuild, eb.Main)
		link.CacheExecutable = true
		a := &Action{
			Mode:    "embed build",
			Package: p,
			Actor:   &embedBuildActor{eb: eb},
			Deps:    []*Action{link},
			Objdir:  b.NewObjdir(),
		}
		a.Target = filepath.Join(a.Objdir, filepath.Base(eb.Name))
		return a
	})
}

func (e *embedBuildActor) Act(b *Builder, ctx context.Context, a *Action) error {
	link := a.Deps[0]
	if !e.eb.Gzip {
		a.built = link.built
		return nil
	}
	sh := b.Shell(a)
	show := func() { sh.ShowCmd("", "gzip -9 -n < %s > %s # internal", link.built, a.Target) }
	if cfg.BuildN {
		show()
		a.built = a.Target
		return nil
	}
	// The gzip output is cached under the link's action ID.
	c := cache.Default()
	id := cache.Subkey(link.actionID, "embedbuild gzip")
	if !cfg.BuildA {
		if file, _, err := cache.GetFile(c, id); err == nil {
			a.built = file
			return nil
		}
	}
	if err := sh.Mkdir(a.Objdir); err != nil {
		return err
	}
	if cfg.BuildX {
		show()
	}
	if err := gzipFile(link.built, a.Target); err != nil {
		return err
	}
	a.built = a.Target
	if f, err := os.Open(a.Target); err == nil {
		c.Put(id, f)
		f.Close()
	}
	return nil
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
