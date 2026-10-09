// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package help

import "cmd/go/internal/base"

var HelpEmbedBuild = &base.Command{
	UsageLine: "embedbuild",
	Short:     "embedding a build of another package",
	Long: `
A //go:embedbuild directive in the header of a Go file, before the package
clause, asks the go command to build a main package and embed the result
as a file:

	//go:embedbuild [-gzip] [-strip] [-tags=list] [-fallback] name [KEY=value...] pkg

The go command builds pkg for the target given by the KEY=value settings
and embeds the result as name, which must fall under one of the package's
//go:embed patterns. GOOS, GOARCH and the GOARCH-specific variable such as
GOAMD64 or GOWASM may be set, and default to the go command's own target.
The inner build is part of the same build: it is cached like any other
step and shows in the output of -n and -x.

The -gzip flag compresses the result with gzip. The -strip flag links
pkg with -s -w. The -tags flag sets the build tags of the inner build,
which does not inherit the outer ones. The inner build never uses cgo,
the race detector or other instrumentation, or the outer -buildmode.

A file named name may exist in the package directory only if the
directive is marked -fallback. That file is what a toolchain without
//go:embedbuild embeds, and what this go command embeds when the
directive is inactive. A directive is inactive when the build flag
-embedbuild=off is set, and in a package outside the modules being
built: the main modules, and the module of a package named on the
command line, as in 'go install pkg@version'. So a dependency never adds
a build to its importers. 'go list -json' reports each directive and,
in its Inactive field, why it is not used.

Only build, install, run and test build the packages of directives.
A package that embeds a build of itself for the same target is an error.
`,
}
