// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dsnet/godoc/internal/doc"
)

type packageInfo struct {
	name    string   // e.g., "tar"
	impPath string   // e.g., "archive/tar"
	dirPath string   // e.g., "/usr/local/go/src/archive/tar"
	files   []string // e.g., ["reader.go", "reader_test.go", ...]

	packages map[string]*packageInfo
}

// loadPackages loads all packages matching pattern and
// returns a single root node representing the package tree.
func loadPackages(pattern string) (*packageInfo, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("go", "list", "-f", `{{printf "%q %q %q %q %q %q %q" .Name .ImportPath .Dir .GoFiles .CgoFiles .TestGoFiles .XTestGoFiles}}`, pattern)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("execute `go list` error: %w\n%v", err, stderr)
	}

	// We need to know the pseudo-source for builtin declarations.
	cmd = exec.Command("go", "list", "-f", `{{printf "%q %q %q %q %q %q %q" .Name .ImportPath .Dir .GoFiles .CgoFiles .TestGoFiles .XTestGoFiles}}`, "builtin")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("execute `go list` error: %w\n%v", err, stderr)
	}

	root := new(packageInfo)
	for {
		line, err := stdout.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return root, err
		}
		in := strings.TrimSuffix(string(line), "\n")
		if len(in) == 0 || err == io.EOF {
			break
		}

		var pkg packageInfo
		in = strings.TrimLeft(in, " ")
		pkg.name, in, err = unquotePrefix(in)
		if err != nil {
			return root, fmt.Errorf("unable to parse `go list` output: %w", err)
		}
		in = strings.TrimLeft(in, " ")
		pkg.impPath, in, err = unquotePrefix(in)
		if err != nil {
			return root, fmt.Errorf("unable to parse `go list` output: %w", err)
		}
		in = strings.TrimLeft(in, " ")
		pkg.dirPath, in, err = unquotePrefix(in)
		if err != nil {
			return root, fmt.Errorf("unable to parse `go list` output: %w", err)
		}
		in = strings.TrimLeft(in, "[] ")
		for len(in) > 0 {
			var file string
			file, in, err = unquotePrefix(in)
			if err != nil {
				return root, fmt.Errorf("unable to parse `go list` output: %w", err)
			}
			pkg.files = append(pkg.files, file)
			in = strings.TrimLeft(in, "[] ")
		}
		sort.Strings(pkg.files)
		root.merge(pkg)
	}
	return root, nil
}

func (root *packageInfo) merge(pkg packageInfo) {
	var dirName string
	suffix := strings.TrimPrefix(strings.TrimPrefix(pkg.impPath, root.impPath), "/")
	if i := strings.IndexByte(suffix, '/'); i >= 0 {
		dirName, suffix = suffix[:i], suffix[i+len("/"):]
	} else {
		dirName, suffix = suffix, ""
	}
	child, ok := root.packages[dirName]
	if !ok {
		if root.packages == nil {
			root.packages = make(map[string]*packageInfo)
		}
		child = &packageInfo{impPath: path.Join(root.impPath, dirName)}
		root.packages[dirName] = child
	}
	if suffix == "" {
		child.name = pkg.name
		child.dirPath = pkg.dirPath
		child.files = pkg.files
	} else {
		child.merge(pkg)
	}
}

func (pkg *packageInfo) resolve(impPath string) *packageInfo {
	for len(impPath) > 0 {
		dirName := impPath
		if i := strings.IndexByte(impPath, '/'); i >= 0 {
			dirName, impPath = impPath[:i], impPath[i+len("/"):]
		} else {
			dirName, impPath = impPath, ""
		}
		pkg = pkg.packages[dirName]
		if pkg == nil {
			return nil
		}
	}
	return pkg
}

func (pkg *packageInfo) walk(visit func(*packageInfo) bool) bool {
	if !visit(pkg) {
		return false
	}
	var names []string
	for name := range pkg.packages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !pkg.packages[name].walk(visit) {
			return false
		}
	}
	return true
}

func (pkg *packageInfo) loadDoc() (*token.FileSet, *doc.Package, error) {
	if len(pkg.files) == 0 {
		return nil, nil, fmt.Errorf("no files present for %q", pkg.impPath)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range pkg.files {
		name = filepath.Join(pkg.dirPath, name)
		src, err := os.ReadFile(name)
		if err != nil {
			return nil, nil, err
		}
		file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, file)
	}

	var noFiltering, noTypeAssociation bool
	if pkg.impPath == "builtin" {
		noFiltering = true
		noTypeAssociation = true
	}

	var m doc.Mode
	if noFiltering {
		m |= doc.AllDecls
	}
	docPkg, err := doc.NewFromFiles(fset, files, pkg.impPath, m)
	if noTypeAssociation {
		for _, t := range docPkg.Types {
			docPkg.Consts, t.Consts = append(docPkg.Consts, t.Consts...), nil
			docPkg.Vars, t.Vars = append(docPkg.Vars, t.Vars...), nil
			docPkg.Funcs, t.Funcs = append(docPkg.Funcs, t.Funcs...), nil
		}
		sort.Slice(docPkg.Funcs, func(i, j int) bool { return docPkg.Funcs[i].Name < docPkg.Funcs[j].Name })
	}
	return fset, docPkg, err
}

func unquotePrefix(in string) (out, rem string, err error) {
	n := quotedPrefixLen(in)
	out, err = strconv.Unquote(in[:n])
	return out, in[n:], err
}

// quotedPrefixLen returns the length of a quoted string at the start of s.
// See http://golang.org/issue/45033.
func quotedPrefixLen(s string) int {
	if len(s) == 0 {
		return len(s)
	}
	switch s[0] {
	case '`':
		for i, r := range s[len("`"):] {
			if r == '`' {
				return len("`") + i + len("`")
			}
		}
	case '"':
		var inEscape bool
		for i, r := range s[len(`"`):] {
			switch {
			case inEscape:
				inEscape = false
			case r == '\\':
				inEscape = true
			case r == '"':
				return len(`"`) + i + len(`"`)
			}
		}
	}
	return len(s)
}
