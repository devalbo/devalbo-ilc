package platform

// BFT — Brute Force Transfer (github.com/devalbo/brute-force-transfer): a whole
// directory tree as ONE human-readable, diffable, git-friendly JSON blob. It is
// the interchange format for `export-fs` / `import-fs` (§7.3), and the reason
// the same bundle works in the terminal, the browser, and on embedded: it is
// text, so it survives any channel that can carry a string.
//
// Format (recursive nodes):
//
//	directory  {"type":"directory","entries":{name: node, …}}
//	text       {"type":"text","content":"…"}                  UTF-8, JSON-escaped
//	binary     {"type":"binary","encoding":"base64","content":"…"}
//
// Entries are alphabetical — that is what makes a bundle byte-stable, so two
// exports of the same tree diff cleanly and the native/wasm parity check can
// compare bundles directly.
//
// Written by hand rather than with encoding/json: the engine must stay
// reflection-free for TinyGo (§7.2), and encoding/json is the canonical example
// of what not to import here. The parser below accepts only the BFT subset —
// objects and strings — which keeps it small enough to be obviously correct.

import (
	"bytes"
	"encoding/base64"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// bftNode is one node of the tree: a directory with named children, or a file
// with bytes. Emission decides text-vs-binary from the content.
type bftNode struct {
	isDir   bool
	entries map[string]*bftNode
	content []byte
}

func newDir() *bftNode { return &bftNode{isDir: true, entries: map[string]*bftNode{}} }

// insert places content at a slash-separated path, creating directories.
func (n *bftNode) insert(path string, content []byte) error {
	parts := strings.Split(path, "/")
	cur := n
	for i, part := range parts {
		if part == "" {
			return errors.New("empty path segment in " + path)
		}
		last := i == len(parts)-1
		if last {
			cur.entries[part] = &bftNode{content: content}
			return nil
		}
		next, ok := cur.entries[part]
		if !ok {
			next = newDir()
			cur.entries[part] = next
		}
		if !next.isDir {
			return errors.New("path conflict: " + part + " is a file in " + path)
		}
		cur = next
	}
	return nil
}

// walk yields every file as (path, content), alphabetically — the same order
// encodeBFT emits, so callers get deterministic results too.
func (n *bftNode) walk(prefix string, fn func(path string, content []byte) error) error {
	for _, name := range sortedKeys(n.entries) {
		child := n.entries[name]
		path := name
		if prefix != "" {
			path = prefix + "/" + name
		}
		if child.isDir {
			if err := child.walk(path, fn); err != nil {
				return err
			}
			continue
		}
		if err := fn(path, child.content); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys(m map[string]*bftNode) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Map iteration is randomized (deliberately so in TinyGo too), and BFT
	// requires alphabetical entries — sorting is what makes bundles byte-stable.
	sort.Strings(keys)
	return keys
}

// encodeBFT renders the tree as pretty-printed BFT JSON. Pretty-printing is part
// of the point: a bundle should be readable and diffable, not a wall of text.
func encodeBFT(root *bftNode) []byte {
	var b bytes.Buffer
	writeNode(&b, root, 0)
	b.WriteByte('\n')
	return b.Bytes()
}

func writeNode(b *bytes.Buffer, n *bftNode, depth int) {
	pad := strings.Repeat("  ", depth)
	inner := pad + "  "
	if n.isDir {
		b.WriteString("{\n")
		b.WriteString(inner + `"type": "directory",` + "\n")
		b.WriteString(inner + `"entries": {`)
		names := sortedKeys(n.entries)
		if len(names) == 0 {
			b.WriteString("}\n" + pad + "}")
			return
		}
		b.WriteString("\n")
		for i, name := range names {
			b.WriteString(inner + "  ")
			writeJSONString(b, name)
			b.WriteString(": ")
			writeNode(b, n.entries[name], depth+2)
			if i < len(names)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(inner + "}\n" + pad + "}")
		return
	}
	b.WriteString("{\n")
	if isTextContent(n.content) {
		b.WriteString(inner + `"type": "text",` + "\n")
		b.WriteString(inner + `"content": `)
		writeJSONString(b, string(n.content))
	} else {
		b.WriteString(inner + `"type": "binary",` + "\n")
		b.WriteString(inner + `"encoding": "base64",` + "\n")
		b.WriteString(inner + `"content": `)
		writeJSONString(b, base64.StdEncoding.EncodeToString(n.content))
	}
	b.WriteString("\n" + pad + "}")
}

// isTextContent decides text vs binary. Valid UTF-8 with no control characters
// other than tab/newline/carriage-return round-trips readably as text; anything
// else goes to base64. Being conservative here costs a little size and buys a
// bundle a human can actually read and diff.
func isTextContent(content []byte) bool {
	if !utf8.Valid(content) {
		return false
	}
	for _, c := range content {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
	}
	return true
}

func writeJSONString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				const hex = "0123456789abcdef"
				b.WriteByte(hex[(r>>4)&0xf])
				b.WriteByte(hex[r&0xf])
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}

// ---- parsing -------------------------------------------------------------
//
// A recursive-descent parser for the BFT subset: objects with string keys whose
// values are objects or strings. No numbers, arrays, booleans, or nulls — the
// format has none, and refusing them keeps the parser small and total.

type bftParser struct {
	src []byte
	pos int
}

func decodeBFT(src []byte) (*bftNode, error) {
	p := &bftParser{src: src}
	p.ws()
	node, err := p.node()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.pos != len(p.src) {
		return nil, p.errf("trailing content after the root node")
	}
	return node, nil
}

func (p *bftParser) node() (*bftNode, error) {
	fields, err := p.object()
	if err != nil {
		return nil, err
	}
	kind, ok := fields.strings["type"]
	if !ok {
		return nil, p.errf(`node is missing "type"`)
	}
	switch kind {
	case "directory":
		dir := newDir()
		if fields.entries != nil {
			dir.entries = fields.entries
		}
		return dir, nil
	case "text":
		content, ok := fields.strings["content"]
		if !ok {
			return nil, p.errf(`text node is missing "content"`)
		}
		return &bftNode{content: []byte(content)}, nil
	case "binary":
		if enc := fields.strings["encoding"]; enc != "base64" {
			return nil, p.errf("unsupported binary encoding " + strconv.Quote(enc))
		}
		content, ok := fields.strings["content"]
		if !ok {
			return nil, p.errf(`binary node is missing "content"`)
		}
		raw, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, p.errf("invalid base64 content")
		}
		return &bftNode{content: raw}, nil
	default:
		return nil, p.errf("unknown node type " + strconv.Quote(kind))
	}
}

type bftFields struct {
	strings map[string]string
	entries map[string]*bftNode
}

func (p *bftParser) object() (*bftFields, error) {
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	fields := &bftFields{strings: map[string]string{}}
	p.ws()
	if p.peek() == '}' {
		p.pos++
		return fields, nil
	}
	for {
		p.ws()
		key, err := p.string()
		if err != nil {
			return nil, err
		}
		p.ws()
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		p.ws()
		if key == "entries" {
			entries, err := p.entries()
			if err != nil {
				return nil, err
			}
			fields.entries = entries
		} else if p.peek() == '{' {
			// An unknown object-valued field (BFT allows comments and may grow
			// others) — skip it rather than failing, so old readers survive new
			// writers.
			if _, err := p.object(); err != nil {
				return nil, err
			}
		} else {
			value, err := p.string()
			if err != nil {
				return nil, err
			}
			fields.strings[key] = value
		}
		p.ws()
		if p.peek() == ',' {
			p.pos++
			continue
		}
		if err := p.expect('}'); err != nil {
			return nil, err
		}
		return fields, nil
	}
}

func (p *bftParser) entries() (map[string]*bftNode, error) {
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	out := map[string]*bftNode{}
	p.ws()
	if p.peek() == '}' {
		p.pos++
		return out, nil
	}
	for {
		p.ws()
		name, err := p.string()
		if err != nil {
			return nil, err
		}
		p.ws()
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		p.ws()
		child, err := p.node()
		if err != nil {
			return nil, err
		}
		out[name] = child
		p.ws()
		if p.peek() == ',' {
			p.pos++
			continue
		}
		if err := p.expect('}'); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func (p *bftParser) string() (string, error) {
	if err := p.expect('"'); err != nil {
		return "", err
	}
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch c {
		case '"':
			p.pos++
			return b.String(), nil
		case '\\':
			p.pos++
			if p.pos >= len(p.src) {
				return "", p.errf("unterminated escape")
			}
			switch p.src[p.pos] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case '/':
				b.WriteByte('/')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'u':
				if p.pos+4 >= len(p.src) {
					return "", p.errf("truncated \\u escape")
				}
				code, err := strconv.ParseUint(string(p.src[p.pos+1:p.pos+5]), 16, 32)
				if err != nil {
					return "", p.errf("invalid \\u escape")
				}
				b.WriteRune(rune(code))
				p.pos += 4
			default:
				return "", p.errf("unknown escape \\" + string(p.src[p.pos]))
			}
			p.pos++
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
	return "", p.errf("unterminated string")
}

func (p *bftParser) ws() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *bftParser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *bftParser) expect(c byte) error {
	if p.peek() != c {
		return p.errf("expected " + strconv.Quote(string(c)))
	}
	p.pos++
	return nil
}

// errf reports the byte offset — a bundle is a document a human may have edited,
// so "where" matters as much as "what".
func (p *bftParser) errf(msg string) error {
	return errors.New("bft: " + msg + " at offset " + strconv.Itoa(p.pos))
}
