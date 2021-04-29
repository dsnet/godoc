// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"sort"
	"strings"

	"github.com/dsnet/godoc/internal/doc"
	"github.com/dsnet/godoc/internal/render"
	"github.com/google/safehtml"
	"github.com/google/safehtml/legacyconversions"
)

// NOTE: The declarations below are copied from
// golang.org/x/pkgsite/internal/godoc/dochtml/dochtml.go@d553d1a7.

// examples is an internal representation of all package examples.
type examples struct {
	List []*example            // sorted by ParentID
	Map  map[string][]*example // keyed by top-level ID (e.g., "NewRing" or "PubSub.Receive") or empty string for package examples
}

// example is an internal representation of a single example.
type example struct {
	*doc.Example
	ID       safehtml.Identifier // ID of example
	ParentID string              // ID of top-level declaration this example is attached to
	Suffix   string              // optional suffix name in title case
}

// collectExamples extracts examples from p
// into the internal examples representation.
func collectExamples(p *doc.Package) *examples {
	exs := &examples{
		List: nil,
		Map:  make(map[string][]*example),
	}
	WalkExamples(p, func(id string, ex *doc.Example) {
		suffix := strings.Title(ex.Suffix)
		ex0 := &example{
			Example:  ex,
			ID:       exampleID(id, suffix),
			ParentID: id,
			Suffix:   suffix,
		}
		exs.List = append(exs.List, ex0)
		exs.Map[id] = append(exs.Map[id], ex0)
	})
	sort.SliceStable(exs.List, func(i, j int) bool {
		// TODO: Break ties by sorting by suffix, unless
		// not needed because of upstream slice order.
		return exs.List[i].ParentID < exs.List[j].ParentID
	})
	return exs
}

// WalkExamples calls fn for each Example in p,
// setting id to the name of the parent structure.
func WalkExamples(p *doc.Package, fn func(id string, ex *doc.Example)) {
	for _, ex := range p.Examples {
		fn("", ex)
	}
	for _, f := range p.Funcs {
		for _, ex := range f.Examples {
			fn(f.Name, ex)
		}
	}
	for _, t := range p.Types {
		for _, ex := range t.Examples {
			fn(t.Name, ex)
		}
		for _, f := range t.Funcs {
			for _, ex := range f.Examples {
				fn(f.Name, ex)
			}
		}
		for _, m := range t.Methods {
			for _, ex := range m.Examples {
				fn(t.Name+"."+m.Name, ex)
			}
		}
	}
}

func exampleID(id, suffix string) safehtml.Identifier {
	switch {
	case id == "" && suffix == "":
		return safehtml.IdentifierFromConstant("example-package")
	case id == "" && suffix != "":
		render.ValidateGoDottedExpr(suffix)
		return legacyconversions.RiskilyAssumeIdentifier("example-package-" + suffix)
	case id != "" && suffix == "":
		render.ValidateGoDottedExpr(id)
		return legacyconversions.RiskilyAssumeIdentifier("example-" + id)
	case id != "" && suffix != "":
		render.ValidateGoDottedExpr(id)
		render.ValidateGoDottedExpr(suffix)
		return legacyconversions.RiskilyAssumeIdentifier("example-" + id + "-" + suffix)
	default:
		panic("unreachable")
	}
}
