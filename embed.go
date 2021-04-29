// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import _ "embed"

//go:embed static/img/favicon.svg
var faviconSVG []byte

//go:embed static/img/favicon.ico
var faviconIco []byte

//go:embed static/js/code.js
var codeJS []byte

//go:embed static/css/style.css
var styleCSS []byte

//go:embed static/html/index.html
var indexHTML string
