// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"archive/tar"
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
)

var pl = fmt.Println
var pf = fmt.Printf

func main() {
	log.SetFlags(log.Lshortfile | log.Ltime)
	experiments := flag.String("experiments", "", "A comma separated list of experimental features (e.g., \"sections,hotlinks,lists\").\n\n"+
		"Valid features:\n"+
		"\tsections             https://golang.org/issue/44447\n"+
		"\thotlinks             https://golang.org/issue/25444\n"+
		"\thotlinks-bracket     https://golang.org/issue/45533 using brackets as delimiters\n"+
		"\thotlinks-backtick    https://golang.org/issue/45533 using backticks and as delimiters\n"+
		"\thotlinks-backquote   https://golang.org/issue/45533 using a backtick and single quote as delimiters\n"+
		"\tlists                https://golang.org/issue/7873#issuecomment-820116651",
	)
	archive := flag.String("archive", "", "The output file for generated archive files. Specify '-' to output to stdout.")
	address := flag.String("address", "0.0.0.0:8080", "The address to serve GoDoc on.")
	flag.Parse()

	for _, experiment := range strings.Split(*experiments, ",") {
		switch experiment {
		case "":
		case "sections":
			log.Fatalf("%v not implemented", experiment)
		case "hotlinks":
			log.Fatalf("%v not implemented", experiment)
		case "hotlinks-bracket":
			log.Fatalf("%v not implemented", experiment)
		case "hotlinks-backtick":
			log.Fatalf("%v not implemented", experiment)
		case "hotlinks-backquote":
			log.Fatalf("%v not implemented", experiment)
		case "hotlinks-verify":
			log.Fatalf("%v not implemented", experiment)
		case "lists":
			log.Fatalf("%v not implemented", experiment)
		default:
			log.Fatalf("unknown experimental feature: %v", experiment)
		}
	}

	// Construct a tree of all packages.
	root, err := loadPackages("all")
	if err != nil {
		log.Fatalf("unable to load all packages: %v", err)
	}

	if *archive != "" {
		if *archive == "" {
			log.Fatal("unknown output, please specify the '-archive' flag")
		}

		// Open the output archive file.
		f := os.Stdout
		if *archive != "-" {
			f, err = os.OpenFile(*archive, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0664)
			if err != nil {
				log.Fatalf("os.OpenFile error: %v", err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					log.Fatalf("os.File.Close error: %v", err)
				}
			}()
		}
		tw := tar.NewWriter(f)
		defer func() {
			if err := tw.Close(); err != nil {
				log.Fatalf("tar.Writer.Close error: %v", err)
			}
		}()

		// Iterate over static files.
		for _, file := range []struct {
			name string
			data []byte
		}{
			{"favicon.ico", faviconIco},
			{"favicon.svg", faviconSVG},
			{"code.js", codeJS},
			{"style.css", styleCSS},
		} {
			hdr := &tar.Header{
				Name: file.name,
				Mode: 0664,
				Size: int64(len(file.data)),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				log.Fatalf("tar.Writer.WriteHeader error: %v", err)
			}
			if _, err := tw.Write(file.data); err != nil {
				log.Fatalf("tar.Writer.Write error: %v", err)
			}
		}

		// Iterate over all packages.
		var bb bytes.Buffer
		root.walk(func(pkg *packageInfo) bool {
			log.Printf("rendering %q", pkg.impPath)
			bb.Reset()
			if err := pkg.renderHTML(&bb); err != nil {
				log.Fatalf("packageInfo.renderHTML error: %v", err)
			}
			hdr := &tar.Header{
				Name: path.Join(pkg.impPath, "index.html"),
				Mode: 0664,
				Size: int64(bb.Len()),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				log.Fatalf("tar.Writer.WriteHeader error: %v", err)
			}
			if _, err := tw.Write(bb.Bytes()); err != nil {
				log.Fatalf("tar.Writer.Write error: %v", err)
			}
			return true
		})
	} else {
		// Best-effort attempt to get the current package or module.
		b, _ := exec.Command("go", "list").Output()
		currentPath := strings.TrimSpace(string(b))
		if currentPath == "" {
			b, _ := exec.Command("go", "list", "-m").Output()
			currentPath = strings.TrimSpace(string(b))
		}
		fmt.Printf("http://%v/%v\n\n", *address, currentPath)

		log.Fatal(http.ListenAndServe(*address, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/favicon.ico":
				w.Header().Set("Content-Type", "image/x-icon")
				w.Write(faviconIco)
				return
			case "/favicon.svg":
				w.Header().Set("Content-Type", "image/svg+xml")
				w.Write(faviconSVG)
				return
			case "/code.js":
				w.Header().Set("Content-Type", "application/javascript")
				w.Write(codeJS)
				return
			case "/style.css":
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
				w.Write(styleCSS)
				return
			default:
				pkg := root.resolve(strings.TrimPrefix(r.URL.Path, "/"))
				if pkg == nil {
					http.NotFound(w, r)
					return
				}

				log.Printf("serving %q", pkg.impPath)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if err := pkg.renderHTML(w); err != nil {
					log.Printf("error rendering %q: %v", pkg.impPath, err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		})))
	}
}
