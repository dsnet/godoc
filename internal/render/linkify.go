// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package render

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"go/scanner"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/safehtml"
	"github.com/google/safehtml/legacyconversions"
	"github.com/google/safehtml/template"
	"github.com/dsnet/godoc/internal/doc"
)

/*
This logic is responsible for converting documentation comments and AST nodes
into formatted HTML. This relies on identifierResolver.toHTML to do the work
of converting words into links.
*/

// TODO(golang.org/issue/17056): Support hiding deprecated declarations.

const (
	// Regexp for URLs.
	// Match any ".,:;?!" within path, but not at end (see #18139, #16565).
	// This excludes some rare yet valid URLs ending in common punctuation
	// in order to allow sentences ending in URLs.
	urlRx = protoPart + `://` + hostPart + pathPart

	// Protocol (e.g. "http").
	protoPart = `(https?|s?ftps?|file|gopher|mailto|nntp)`
	// Host (e.g. "www.example.com" or "[::1]:8080").
	hostPart = `([a-zA-Z0-9_@\-.\[\]:]+)`
	// Optional path, query, fragment (e.g. "/path/index.html?q=foo#bar").
	pathPart = `([.,:;?!]*[a-zA-Z0-9$'()*+&#=@~_/\-\[\]%])*`

	// Regexp for Go identifiers.
	identRx     = `[\pL_][\pL_0-9]*`
	qualIdentRx = identRx + `(\.` + identRx + `)*`

	// Regexp for RFCs.
	rfcRx = `RFC\s+(\d{3,5})(,?\s+[Ss]ection\s+(\d+(\.\d+)*))?`
)

var (
	matchRx     = regexp.MustCompile(urlRx + `|` + rfcRx + `|` + qualIdentRx)
	badAnchorRx = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

type docData struct {
	Elements          []docElement
	DisablePermalinks bool
	EnableCommandTOC  bool
}

type docElement struct {
	IsHeading   bool
	IsPreformat bool
	// for paragraph and preformat
	Body safehtml.HTML
	// for heading
	Title string
	ID    safehtml.Identifier
}

func (r *Renderer) declHTML(doc string, decl ast.Decl, extractLinks bool) (out struct{ Doc, Decl safehtml.HTML }) {
	dids := newDeclIDs(decl)
	idr := &identifierResolver{r.pids, dids, r.packageURL}
	if doc != "" {
		var els []docElement
		inLinks := false
		for _, blk := range docToBlocks(doc) {
			var el docElement
			switch blk := blk.(type) {
			case *paragraph:
				if inLinks {
					r.links = append(r.links, parseLinks(blk.lines)...)
				} else {
					el.Body = r.linesToHTML(blk.lines, idr)
					els = append(els, el)
				}
			case *preformat:
				if inLinks {
					r.links = append(r.links, parseLinks(blk.lines)...)
				} else {
					el.IsPreformat = true
					el.Body = r.linesToHTML(blk.lines, nil)
					els = append(els, el)
				}
			case *heading:
				if extractLinks && blk.title == "Links" {
					inLinks = true
				} else {
					inLinks = false
					el.IsHeading = true
					el.Title = blk.title
					id := badAnchorRx.ReplaceAllString(blk.title, "_")
					el.ID = safehtml.IdentifierFromConstantPrefix("hdr", id)
					els = append(els, el)
				}
			}
		}
		out.Doc = ExecuteToHTML(r.docTmpl, docData{Elements: els,
			DisablePermalinks: r.disablePermalinks, EnableCommandTOC: r.enableCommandTOC})
	}
	if decl != nil {
		out.Decl = r.formatDeclHTML(decl, idr)
	}
	return out
}

// parseLinks extracts links from lines.
func parseLinks(lines []string) []Link {
	var links []Link
	for _, l := range lines {
		if link := parseLink(l); link != nil {
			links = append(links, *link)
		}
	}
	return links
}

// If line is of the form "- title, url", then parseLink returns
// a Link with the title and url. Otherwise it returns nil.
// The line already has leading whitespace trimmed.
func parseLink(line string) *Link {
	if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "-\t") {
		return nil
	}
	parts := strings.SplitN(line[2:], ",", 2)
	if len(parts) != 2 {
		return nil
	}
	text := strings.TrimSpace(parts[0])
	href := strings.TrimSpace(parts[1])
	return &Link{
		Text: text,
		Href: href,
	}
}

func (r *Renderer) linesToHTML(lines []string, idr *identifierResolver) safehtml.HTML {
	newline := safehtml.HTMLEscaped("\n")
	htmls := make([]safehtml.HTML, 0, 2*len(lines))
	for _, l := range lines {
		htmls = append(htmls, r.formatLineHTML(l, idr))
		htmls = append(htmls, newline)
	}
	return safehtml.HTMLConcat(htmls...)
}

func (r *Renderer) codeString(ex *doc.Example) (string, error) {
	if ex == nil || ex.Code == nil {
		return "", errors.New("Please include an example with code")
	}
	var buf bytes.Buffer

	if ex.Play != nil {
		if err := format.Node(&buf, r.fset, ex.Play); err != nil {
			return "", err
		}
	} else {
		n := &printer.CommentedNode{
			Node:     ex.Code,
			Comments: ex.Comments,
		}
		if err := format.Node(&buf, r.fset, n); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}

func (r *Renderer) codeHTML(ex *doc.Example) safehtml.HTML {
	codeStr, err := r.codeString(ex)
	if err != nil {
		return template.MustParseAndExecuteToHTML(`<pre class="Documentation-exampleCode">Error rendering example code.</pre>`)
	}
	return codeHTML(codeStr, r.exampleTmpl)
}

type codeElement struct {
	Text    string
	Comment bool
}

func codeHTML(src string, codeTmpl *template.Template) safehtml.HTML {
	var els []codeElement
	// If code is an *ast.BlockStmt, then trim the braces.
	var indent string
	if len(src) >= 4 && strings.HasPrefix(src, "{\n") && strings.HasSuffix(src, "\n}") {
		src = strings.Trim(src[2:len(src)-2], "\n")
		indent = src[:indentLength(src)]
		if len(indent) > 0 {
			src = strings.TrimPrefix(src, indent) // handle remaining indents later
		}
	}

	// Scan through the source code, adding comment spans for comments,
	// and stripping the trailing example output.
	var lastOffset int        // last src offset copied to output buffer
	var outputOffset int = -1 // index in els of last output comment
	var s scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	s.Init(file, []byte(src), nil, scanner.ScanComments)
	indent = "\n" + indent // prepend newline for easier search-and-replace.
scan:
	for {
		p, tok, lit := s.Scan()
		offset := file.Offset(p) // current offset into source file
		prev := src[lastOffset:offset]
		prev = strings.Replace(prev, indent, "\n", -1)
		els = append(els, codeElement{prev, false})
		lastOffset = offset
		switch tok {
		case token.EOF:
			break scan
		case token.COMMENT:
			if exampleOutputRx.MatchString(lit) {
				outputOffset = len(els)
			}
			lit = strings.Replace(lit, indent, "\n", -1)
			els = append(els, codeElement{lit, true})
			lastOffset += len(lit)
		case token.STRING:
			// Avoid replacing indents in multi-line string literals.
			els = append(els, codeElement{lit, false})
			lastOffset += len(lit)
		}
	}

	if outputOffset >= 0 {
		els = els[:outputOffset]
	}
	// Trim trailing newlines.
	if len(els) > 0 {
		els[len(els)-1].Text = strings.TrimRight(els[len(els)-1].Text, "\n")
	}
	return ExecuteToHTML(codeTmpl, els)
}

// formatLineHTML formats the line as HTML-annotated text.
// URLs and Go identifiers are linked to corresponding declarations.
func (r *Renderer) formatLineHTML(line string, idr *identifierResolver) safehtml.HTML {
	var htmls []safehtml.HTML
	var lastChar, nextChar byte
	var numQuotes int

	addLink := func(href, text string) {
		htmls = append(htmls, ExecuteToHTML(LinkTemplate, Link{Href: href, Text: text}))
	}

	line = convertQuotes(line)
	for len(line) > 0 {
		m0, m1 := len(line), len(line)
		if m := matchRx.FindStringIndex(line); m != nil {
			m0, m1 = m[0], m[1]
		}
		if m0 > 0 {
			nonWord := line[:m0]
			htmls = append(htmls, safehtml.HTMLEscaped(nonWord))
			lastChar = nonWord[len(nonWord)-1]
			numQuotes += countQuotes(nonWord)
		}
		if m1 > m0 {
			word := line[m0:m1]
			nextChar = 0
			if m1 < len(line) {
				nextChar = line[m1]
			}

			// Reduce false-positives by having a list of allowed
			// characters preceding and succeeding an identifier.
			// Also, forbid ID linking within unbalanced quotes on same line.
			validPrefix := strings.IndexByte("\x00 \t()[]*\n", lastChar) >= 0
			validSuffix := strings.IndexByte("\x00 \t()[]:;,.'\n", nextChar) >= 0
			forbidLinking := !validPrefix || !validSuffix || numQuotes%2 != 0

			// TODO: Should we provide hotlinks for related packages?

			switch {
			case strings.Contains(word, "://"):
				// Forbid closing brackets without prior opening brackets.
				// See https://golang.org/issue/22285.
				if i := strings.IndexByte(word, ')'); i >= 0 && i < strings.IndexByte(word, '(') {
					m1 = m0 + i
					word = line[m0:m1]
				}
				if i := strings.IndexByte(word, ']'); i >= 0 && i < strings.IndexByte(word, '[') {
					m1 = m0 + i
					word = line[m0:m1]
				}

				// Require balanced pairs of parentheses.
				// See https://golang.org/issue/5043.
				for i := 0; strings.Count(word, "(") != strings.Count(word, ")") && i < 10; i++ {
					m1 = strings.LastIndexAny(line[:m1], "()")
					word = line[m0:m1]
				}
				for i := 0; strings.Count(word, "[") != strings.Count(word, "]") && i < 10; i++ {
					m1 = strings.LastIndexAny(line[:m1], "[]")
					word = line[m0:m1]
				}

				addLink(word, word)
			// Match "RFC ..." to link RFCs.
			case strings.HasPrefix(word, "RFC") && len(word) > 3 && unicode.IsSpace(rune(word[3])):
				// Strip all characters except for letters, numbers, and '.' to
				// obtain RFC fields.
				rfcFields := strings.FieldsFunc(word, func(c rune) bool {
					return !unicode.IsLetter(c) && !unicode.IsNumber(c) && c != '.'
				})
				if len(rfcFields) >= 4 {
					// RFC x Section y
					addLink(fmt.Sprintf("https://rfc-editor.org/rfc/rfc%s.html#section-%s", rfcFields[1], rfcFields[3]), word)
				} else if len(rfcFields) >= 2 {
					// RFC x
					addLink(fmt.Sprintf("https://rfc-editor.org/rfc/rfc%s.html", rfcFields[1]), word)
				}
			case !forbidLinking && !r.disableHotlinking && idr != nil: // && numQuotes%2 == 0:
				htmls = append(htmls, idr.toHTML(word))
			default:
				htmls = append(htmls, safehtml.HTMLEscaped(word))
			}
			numQuotes += countQuotes(word)
		}
		line = line[m1:]
	}
	return safehtml.HTMLConcat(htmls...)
}

func ExecuteToHTML(tmpl *template.Template, data interface{}) safehtml.HTML {
	h, err := tmpl.ExecuteToHTML(data)
	if err != nil {
		return safehtml.HTMLEscaped("[" + err.Error() + "]")
	}
	return h
}

func countQuotes(s string) int {
	n := -1 // loop always iterates at least once
	for i := len(s); i >= 0; i = strings.LastIndexAny(s[:i], `"“”`) {
		n++
	}
	return n
}

// formatDeclHTML formats the decl as HTML-annotated source code for the
// provided decl. Type identifiers are linked to corresponding declarations.
func (r *Renderer) formatDeclHTML(decl ast.Decl, idr *identifierResolver) safehtml.HTML {
	// Generate all anchor points and links for the given decl.
	anchorPointsMap := generateAnchorPoints(decl)
	anchorLinksMap := generateAnchorLinks(idr, decl)

	// Convert the maps (keyed by *ast.Ident) to slices of idKinds or URLs.
	//
	// This relies on the ast.Inspect and scanner.Scanner both
	// visiting *ast.Ident and token.IDENT nodes in the same order.
	var anchorPoints []idKind
	var anchorLinks []string
	ast.Inspect(decl, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok {
			anchorPoints = append(anchorPoints, anchorPointsMap[id])
			anchorLinks = append(anchorLinks, anchorLinksMap[id])
		}
		return true
	})

	// Trim large string literals and composite literals.
	const (
		maxStringSize = 125
		maxElements   = 100
	)
	decl = rewriteDecl(decl, maxStringSize, maxElements)
	// Format decl as Go source code file.
	p := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 4}
	var b bytes.Buffer
	p.Fprint(&b, r.fset, decl)
	src := b.Bytes()
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), b.Len())

	// anchorLines is a list of anchor IDs that should be placed for each line.
	// lineTypes is a list of the type (e.g., comment or code) of each line.
	type lineType byte
	const codeType, commentType lineType = 1 << 0, 1 << 1 // may OR together
	numLines := bytes.Count(src, []byte("\n")) + 1
	anchorLines := make([][]idKind, numLines)
	lineTypes := make([]lineType, numLines)
	htmlLines := make([][]safehtml.HTML, numLines)

	// Scan through the source code, appropriately annotating it with HTML spans
	// for comments, and HTML links and anchors for relevant identifiers.
	var idIdx int      // current index in anchorPoints and anchorLinks
	var lastOffset int // last src offset copied to output buffer
	var s scanner.Scanner
	s.Init(file, src, nil, scanner.ScanComments)
scan:
	for {
		p, tok, lit := s.Scan()
		line := file.Line(p) - 1 // current 0-indexed line number
		offset := file.Offset(p) // current offset into source file
		tokType := codeType      // current token type (assume source code)

		// Add traversed bytes from src to the appropriate line.
		prevLines := strings.SplitAfter(string(src[lastOffset:offset]), "\n")
		for i, ln := range prevLines {
			n := line - len(prevLines) + i + 1
			if n < 0 { // possible at EOF
				n = 0
			}
			htmlLines[n] = append(htmlLines[n], safehtml.HTMLEscaped(ln))
		}

		lastOffset = offset
		switch tok {
		case token.EOF:
			break scan
		case token.COMMENT:
			tokType = commentType
			htmlLines[line] = append(htmlLines[line],
				template.MustParseAndExecuteToHTML(`<span class="comment">`),
				r.formatLineHTML(lit, idr),
				template.MustParseAndExecuteToHTML(`</span>`))
			lastOffset += len(lit)
		case token.IDENT:
			if idIdx < len(anchorPoints) && anchorPoints[idIdx].ID.String() != "" {
				anchorLines[line] = append(anchorLines[line], anchorPoints[idIdx])
			}
			if idIdx < len(anchorLinks) && anchorLinks[idIdx] != "" {
				htmlLines[line] = append(htmlLines[line], ExecuteToHTML(LinkTemplate, Link{Href: anchorLinks[idIdx], Text: lit}))
				lastOffset += len(lit)
			}
			idIdx++
		}
		for i := strings.Count(strings.TrimSuffix(lit, "\n"), "\n"); i >= 0; i-- {
			lineTypes[line+i] |= tokType
		}
	}

	// Move anchor points up to the start of a comment
	// if the next line has no anchors.
	for i := range anchorLines {
		if i+1 == len(anchorLines) || len(anchorLines[i+1]) == 0 {
			j := i
			for j > 0 && lineTypes[j-1] == commentType {
				j--
			}
			anchorLines[i], anchorLines[j] = anchorLines[j], anchorLines[i]
		}
	}

	// Emit anchor IDs and data-kind attributes for each relevant line.
	var htmls []safehtml.HTML
	for line, iks := range anchorLines {
		inAnchor := false
		for _, ik := range iks {
			// Attributes for types and functions are handled in the template
			// that generates the full documentation HTML.
			if ik.Kind == "function" || ik.Kind == "type" {
				continue
			}
			// Top-level methods are handled in the template, but interface methods
			// are handled here.
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil {
				continue
			}
			htmls = append(htmls, ExecuteToHTML(anchorTemplate, ik))
			inAnchor = true
		}
		htmls = append(htmls, htmlLines[line]...)
		if inAnchor {
			htmls = append(htmls, template.MustParseAndExecuteToHTML("</span>"))
		}
	}
	return safehtml.HTMLConcat(htmls...)
}

var anchorTemplate = template.Must(template.New("anchor").Parse(`<span id="{{.ID}}" data-kind="{{.Kind}}">`))

// rewriteDecl rewrites n by removing strings longer than maxStringSize and
// composite literals longer than maxElements.
func rewriteDecl(n ast.Decl, maxStringSize, maxElements int) ast.Decl {
	v := &rewriteVisitor{maxStringSize, maxElements}
	ast.Walk(v, n)
	return n
}

type rewriteVisitor struct {
	maxStringSize, maxElements int
}

func (v *rewriteVisitor) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.ValueSpec:
		for _, val := range n.Values {
			v.rewriteLongValue(val, &n.Comment)
		}
	case *ast.Field:
		if n.Tag != nil {
			v.rewriteLongValue(n.Tag, &n.Comment)
		}
	}
	return v
}

func (v *rewriteVisitor) rewriteLongValue(n ast.Node, pcg **ast.CommentGroup) {
	switch n := n.(type) {
	case *ast.BasicLit:
		if n.Kind != token.STRING {
			return
		}
		size := len(n.Value) - 2 // subtract quotation marks
		if size <= v.maxStringSize {
			return
		}
		addComment(pcg, n.ValuePos, fmt.Sprintf("/* %d-byte string literal not displayed */", size))
		if len(n.Value) == 0 {
			// Impossible, but avoid the panic just in case.
			return
		}
		if quote := n.Value[0]; quote == '`' {
			n.Value = "``"
		} else {
			n.Value = `""`
		}
	case *ast.CompositeLit:
		if len(n.Elts) > v.maxElements {
			addComment(pcg, n.Lbrace, fmt.Sprintf("/* %d elements not displayed */", len(n.Elts)))
			n.Elts = n.Elts[:0]
		}
	}
}

func addComment(cg **ast.CommentGroup, pos token.Pos, text string) {
	if *cg == nil {
		*cg = &ast.CommentGroup{}
	}
	(*cg).List = append((*cg).List, &ast.Comment{Slash: pos, Text: text})
}

// An idKind holds an anchor ID and the kind of the identifier being anchored.
// The valid kinds are: "constant", "variable", "type", "function", "method" and "field".
type idKind struct {
	ID   safehtml.Identifier
	Kind string
}

// SafeGoID constructs a safe identifier from a Go symbol or dotted concatenation of symbols
// (e.g. "Time.Equal").
func SafeGoID(s string) safehtml.Identifier {
	ValidateGoDottedExpr(s)
	return legacyconversions.RiskilyAssumeIdentifier(s)
}

var badIDRx = regexp.MustCompile(`[^_\pL\pN.]`)

// ValidateGoDottedExpr panics if s contains characters other than '.' plus the valid Go identifier characters.
func ValidateGoDottedExpr(s string) {
	if badIDRx.MatchString(s) {
		panic(fmt.Sprintf("invalid identifier characters: %q", s))
	}
}

// generateAnchorPoints returns a mapping of *ast.Ident objects to the
// qualified ID that should be set as an anchor point, as well as the kind
// of identifer, used in the data-kind attribute.
func generateAnchorPoints(decl ast.Decl) map[*ast.Ident]idKind {
	m := map[*ast.Ident]idKind{}
	switch decl := decl.(type) {
	case *ast.GenDecl:
		for _, sp := range decl.Specs {
			switch decl.Tok {
			case token.CONST, token.VAR:
				kind := "constant"
				if decl.Tok == token.VAR {
					kind = "variable"
				}
				for _, name := range sp.(*ast.ValueSpec).Names {
					m[name] = idKind{SafeGoID(name.Name), kind}
				}
			case token.TYPE:
				ts := sp.(*ast.TypeSpec)
				m[ts.Name] = idKind{SafeGoID(ts.Name.Name), "type"}

				var fs []*ast.Field
				var kind string
				switch tx := ts.Type.(type) {
				case *ast.StructType:
					fs = tx.Fields.List
					kind = "field"
				case *ast.InterfaceType:
					fs = tx.Methods.List
					kind = "method"
				}
				for _, f := range fs {
					for _, id := range f.Names {
						m[id] = idKind{SafeGoID(ts.Name.String() + "." + id.String()), kind}
					}
					// if f.Names == nil, we have an embedded struct field or embedded
					// interface.
					//
					// Don't generate anchor points for embedded interfaces. They
					// aren't interesting in and of themselves; they just represent an
					// additional list of methods added to the interface.
					//
					// Do generate anchor points for embedded fields: they are
					// interesting, because their names can be used in selector
					// expressions and struct literals.
					if f.Names == nil && kind == "field" {
						// The name of an embedded field is the type name.
						typeName, id := nodeName(f.Type)
						typeName = typeName[strings.LastIndexByte(typeName, '.')+1:]
						m[id] = idKind{SafeGoID(ts.Name.String() + "." + typeName), kind}
					}
				}
			}
		}
	case *ast.FuncDecl:
		anchorID := decl.Name.Name
		kind := "function"
		if decl.Recv != nil && len(decl.Recv.List) > 0 {
			recvName, _ := nodeName(decl.Recv.List[0].Type)
			recvName = recvName[strings.LastIndexByte(recvName, '.')+1:]
			anchorID = recvName + "." + anchorID
			kind = "method"
		}
		m[decl.Name] = idKind{SafeGoID(anchorID), kind}
	}
	return m
}

// generateAnchorLinks returns a mapping of *ast.Ident objects to the URL
// that the identifier should link to.
func generateAnchorLinks(idr *identifierResolver, decl ast.Decl) map[*ast.Ident]string {
	m := map[*ast.Ident]string{}
	ignore := map[ast.Node]bool{}
	ast.Inspect(decl, func(node ast.Node) bool {
		if ignore[node] {
			return false
		}
		switch node := node.(type) {
		case *ast.SelectorExpr:
			// Package qualified identifier (e.g., "io.EOF").
			if prefix, _ := node.X.(*ast.Ident); prefix != nil {
				if obj := prefix.Obj; obj != nil && obj.Kind == ast.Pkg {
					if spec, _ := obj.Decl.(*ast.ImportSpec); spec != nil {
						if path, err := strconv.Unquote(spec.Path.Value); err == nil {
							// Register two links, one for the package
							// and one for the qualified identifier.
							m[prefix] = idr.toURL(path, "")
							m[node.Sel] = idr.toURL(path, node.Sel.Name)
							return false
						}
					}
				}
			}
		case *ast.Ident:
			if node.Obj == nil && doc.IsPredeclared(node.Name) {
				m[node] = idr.toURL("builtin", node.Name)
			} else if node.Obj != nil && idr.topLevelDecls[node.Obj.Decl] {
				m[node] = "#" + node.Name
			}
		case *ast.FuncDecl:
			ignore[node.Name] = true // E.g., "func NoLink() int"
		case *ast.TypeSpec:
			ignore[node.Name] = true // E.g., "type NoLink int"
		case *ast.ValueSpec:
			for _, n := range node.Names {
				ignore[n] = true // E.g., "var NoLink1, NoLink2 int"
			}
		case *ast.AssignStmt:
			for _, n := range node.Lhs {
				ignore[n] = true // E.g., "NoLink1, NoLink2 := 0, 1"
			}
		}
		return true
	})
	return m
}

const (
	ulquo = "“"
	urquo = "”"
)

var unicodeQuoteReplacer = strings.NewReplacer("``", ulquo, "''", urquo)

// convertQuotes turns `` into “ and '' into ”.
func convertQuotes(text string) string {
	return unicodeQuoteReplacer.Replace(text)
}
