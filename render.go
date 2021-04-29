// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"go/ast"
	"go/token"
	"io"
	"path"
	"reflect"
	"sort"

	"github.com/dsnet/godoc/internal/doc"
	"github.com/dsnet/godoc/internal/render"
	"github.com/google/safehtml"
	"github.com/google/safehtml/template"
)

func (pkg *packageInfo) renderHTML(w io.Writer) error {
	var name string
	var docPkg *doc.Package
	exs := new(examples)
	funcMap := map[string]interface{}{
		"safe_id": render.SafeGoID,
		// "safe_script": legacyconversions.RiskilyAssumeScript,
	}
	if len(pkg.files) > 0 {
		var fset *token.FileSet
		var err error
		fset, docPkg, err = pkg.loadDoc()
		if err != nil {
			return err
		}
		exs = collectExamples(docPkg)

		r := render.New(context.Background(), fset, docPkg, &render.Options{
			PackageURL: func(path string) (url string) {
				return "/" + path
			},
			DisableHotlinking: true,
		})
		funcMap["render_synopsis"] = r.Synopsis
		funcMap["render_doc"] = r.DocHTML
		funcMap["render_decl"] = r.DeclHTML
		funcMap["render_code"] = r.CodeHTML
		name = docPkg.Name
	} else {
		name = path.Base(pkg.impPath)
		if name == "." {
			name = "/"
		}
	}

	var subDirs []string
	for dir := range pkg.packages {
		subDirs = append(subDirs, dir)
	}
	sort.Strings(subDirs)

	return template.Must(htmlPackage.Clone()).Funcs(funcMap).Execute(w, struct {
		*doc.Package
		ImpPath  string
		Name     string
		Examples *examples
		SubDirs  []string
	}{docPkg, pkg.impPath, name, exs, subDirs})
}

var htmlPackage = func() *template.Template {
	t := template.New("package").Funcs(
		map[string]interface{}{
			"ternary": func(q, a, b interface{}) interface{} {
				v := reflect.ValueOf(q)
				vz := reflect.New(v.Type()).Elem()
				if reflect.DeepEqual(v.Interface(), vz.Interface()) {
					return b
				}
				return a
			},
			"render_synopsis": func(ast.Decl) (_ string) { return },
			"render_doc":      func(string) (_ safehtml.HTML) { return },
			"render_decl":     func(string, ast.Decl) (_ [2]safehtml.HTML) { return },
			"render_code":     func(interface{}) (_ safehtml.HTML) { return },
			"safe_id":         func(string) (_ safehtml.Identifier) { return },
			"safe_script":     func(string) (_ safehtml.Script) { return },
		},
	)

	// Unfortunately, safehtml/template makes it impossible to statically parse
	// from a non-literal, which inter-operates poorly with go:embed.
	// Use Go reflection to call Parse and work around this safety feature.
	parse := reflect.ValueOf(t).MethodByName("Parse")
	in := []reflect.Value{reflect.ValueOf(indexHTML).Convert(parse.Type().In(0))}
	out := parse.Call(in)
	t, _ = out[0].Interface().(*template.Template)
	err, _ := out[1].Interface().(error)
	return template.Must(t, err)
}()
