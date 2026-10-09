// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package imports

import (
	"cmd/go/internal/cfg"
	"fmt"
	"sync"
)

// Tags returns a set of build tags that are true for the target platform.
// It includes GOOS, GOARCH, the compiler, possibly "cgo",
// release tags like "go1.13", and user-specified build tags.
func Tags() map[string]bool {
	c := &cfg.BuildContext
	key := fmt.Sprintf("%s/%s/%s/%v/%q/%q/%q", c.GOOS, c.GOARCH, c.Compiler, c.CgoEnabled, c.BuildTags, c.ToolTags, c.ReleaseTags)
	if tags, ok := tagsCache.Load(key); ok {
		return tags.(map[string]bool)
	}
	tags, _ := tagsCache.LoadOrStore(key, loadTags())
	return tags.(map[string]bool)
}

// tagsCache holds a tag set per build context, since a //go:embedbuild
// directive loads packages for a second target.
var tagsCache sync.Map

func loadTags() map[string]bool {
	tags := map[string]bool{
		cfg.BuildContext.GOOS:     true,
		cfg.BuildContext.GOARCH:   true,
		cfg.BuildContext.Compiler: true,
	}
	if cfg.BuildContext.CgoEnabled {
		tags["cgo"] = true
	}
	for _, tag := range cfg.BuildContext.BuildTags {
		tags[tag] = true
	}
	for _, tag := range cfg.BuildContext.ToolTags {
		tags[tag] = true
	}
	for _, tag := range cfg.BuildContext.ReleaseTags {
		tags[tag] = true
	}
	return tags
}

// AnyTags returns a special set of build tags that satisfy nearly all
// build tag expressions. Only "ignore" and malformed build tag requirements
// are considered false.
func AnyTags() map[string]bool {
	return anyTagsOnce()
}

var anyTagsOnce = sync.OnceValue(func() map[string]bool {
	return map[string]bool{"*": true}
})
