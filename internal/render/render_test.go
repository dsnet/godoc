// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package render

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dsnet/godoc/internal/doc"
)

var (
	pkgIO, _          = mustLoadPackage("io")
	pkgOS, _          = mustLoadPackage("os")
	pkgTime, fsetTime = mustLoadPackage("time")
	pkgTar, _         = mustLoadPackage("archive/tar")
)

func mustLoadPackage(path string) (*doc.Package, *token.FileSet) {
	// simpleImporter is used by ast.NewPackage.
	simpleImporter := func(imports map[string]*ast.Object, pkgPath string) (*ast.Object, error) {
		pkg := imports[pkgPath]
		if pkg == nil {
			pkgName := pkgPath[strings.LastIndex(pkgPath, "/")+1:]
			pkg = ast.NewObj(ast.Pkg, pkgName)
			pkg.Data = ast.NewScope(nil) // required for or dot-imports
			imports[pkgPath] = pkg
		}
		return pkg, nil
	}

	srcName := filepath.Base(path) + ".go"
	code, err := ioutil.ReadFile(filepath.Join("testdata", srcName))
	if err != nil {
		panic(err)
	}

	fset := token.NewFileSet()
	pkgFiles := make(map[string]*ast.File)
	astFile, _ := parser.ParseFile(fset, srcName, code, parser.ParseComments)
	pkgFiles[srcName] = astFile
	astPkg, _ := ast.NewPackage(fset, pkgFiles, simpleImporter, nil)
	return doc.New(astPkg, path, 0), fset
}

func TestDocToBlocks(t *testing.T) {
	tests := []struct {
		in   string
		want []block
	}{{
		in:   `This is a sentence.`,
		want: []block{&paragraph{lines{"This is a sentence."}}},
	}, {
		in:   `    This is a sentence.`,
		want: []block{&paragraph{lines{"This is a sentence."}}},
	}, {
		in: `
			Some code:
				func main() {}
			`,
		want: []block{
			&paragraph{lines{"Some code:"}},
			&preformat{lines{"func main() {}"}},
		},
	}, {
		in: `
			The quick brown fox jumped over the lazy dog.
			This is another sentence. La de dah!

			This is a paragraph`,
		want: []block{
			&paragraph{lines{
				"The quick brown fox jumped over the lazy dog.",
				"This is another sentence. La de dah!",
			}},
			&paragraph{lines{"This is a paragraph"}},
		},
	}, {
		in: `
			The quick brown fox jumped over the lazy dog.
			This is another sentence. La de dah!

			This is a heading

			This is a paragraph.`,
		want: []block{
			&paragraph{lines{
				"The quick brown fox jumped over the lazy dog.",
				"This is another sentence. La de dah!",
			}},
			&heading{"This is a heading"},
			&paragraph{lines{"This is a paragraph."}},
		},
	}, {
		in: `
			This is not a heading

			The quick brown fox jumped over the lazy dog.
			This is another sentence. La de dah!

			This is not a heading

				func main() {}

			This is not a heading`,
		want: []block{
			&paragraph{lines{"This is not a heading"}},
			&paragraph{lines{
				"The quick brown fox jumped over the lazy dog.",
				"This is another sentence. La de dah!",
			}},
			&paragraph{lines{"This is not a heading"}},
			&preformat{lines{"func main() {}"}},
			&paragraph{lines{"This is not a heading"}},
		},
	}, {
		in: `
			Xattrs stores extended attributes as PAX records under the
			"SCHILY.xattr." namespace.

			The following are semantically equivalent:
			    h.Xattrs[key] = value
			    h.PAXRecords["SCHILY.xattr."+key] = value

			When Writer.WriteHeader is called, the contents of Xattrs will take
			precedence over those in PAXRecords.

			Deprecated: Use PAXRecords instead.`,
		want: []block{
			&paragraph{lines{
				"Xattrs stores extended attributes as PAX records under the",
				`"SCHILY.xattr." namespace.`,
			}},
			&paragraph{lines{"The following are semantically equivalent:"}},
			&preformat{lines{
				`h.Xattrs[key] = value`,
				`h.PAXRecords["SCHILY.xattr."+key] = value`,
			}},
			&paragraph{lines{
				"When Writer.WriteHeader is called, the contents of Xattrs will take",
				"precedence over those in PAXRecords.",
			}},
			&paragraph{lines{"Deprecated: Use PAXRecords instead."}},
		},
	}, {
		in: `
			Package testing provides support for automated testing of Go packages.

			Benchmarks

			Functions of the form
				    func BenchmarkXxx(*testing.B)
			are considered benchmarks, and are executed by the "go test" command when
			its -bench flag is provided. Benchmarks are run sequentially.

			For a description of the testing flags, see
			https://golang.org/cmd/go/#hdr-Description_of_testing_flags.

			A sample benchmark function looks like this:
				    func BenchmarkHello(b *testing.B) {
				    	for i := 0; i < b.N; i++ {
				    		fmt.Sprintf("hello")
				    	}
				    }

			The benchmark function must run the target code b.N times.
			During benchmark execution, b.N is adjusted until the benchmark function lasts
			long enough to be timed reliably. The output
				    BenchmarkHello    10000000    282 ns/op
			means that the loop ran 10000000 times at a speed of 282 ns per loop.`,
		want: []block{
			&paragraph{lines{"Package testing provides support for automated testing of Go packages."}},
			&heading{"Benchmarks"},
			&paragraph{lines{"Functions of the form"}},
			&preformat{lines{"func BenchmarkXxx(*testing.B)"}},
			&paragraph{lines{
				`are considered benchmarks, and are executed by the "go test" command when`,
				"its -bench flag is provided. Benchmarks are run sequentially.",
			}},
			&paragraph{lines{
				"For a description of the testing flags, see",
				"https://golang.org/cmd/go/#hdr-Description_of_testing_flags.",
			}},
			&paragraph{lines{"A sample benchmark function looks like this:"}},
			&preformat{lines{
				`func BenchmarkHello(b *testing.B) {`,
				`	for i := 0; i < b.N; i++ {`,
				`		fmt.Sprintf("hello")`,
				`	}`,
				`}`,
			}},
			&paragraph{lines{
				"The benchmark function must run the target code b.N times.",
				"During benchmark execution, b.N is adjusted until the benchmark function lasts",
				"long enough to be timed reliably. The output",
			}},
			&preformat{lines{"BenchmarkHello    10000000    282 ns/op"}},
			&paragraph{lines{"means that the loop ran 10000000 times at a speed of 282 ns per loop."}},
		},
	}, {
		in: `
			See https://golang.org/s/go14customimport for details.

			Modules, module versions, and more

			Modules are how Go manages dependencies.

			A module is a collection of packages that are released, versioned, and distributed together. Modules may be downloaded directly from version control repositories or from module proxy servers.`,
		want: []block{
			&paragraph{lines{"See https://golang.org/s/go14customimport for details."}},
			&heading{"Modules, module versions, and more"},
			&paragraph{lines{"Modules are how Go manages dependencies."}},
			&paragraph{lines{"A module is a collection of packages that are released, versioned, and distributed together. Modules may be downloaded directly from version control repositories or from module proxy servers."}},
		},
	}}

	for i, tt := range tests {
		got := docToBlocks(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("test %d, docToBlocks:\ngot  %v\nwant %v", i, got, tt.want)
		}
	}
}
