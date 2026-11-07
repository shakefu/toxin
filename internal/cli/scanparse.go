package cli

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
)

// call is one recorded toxin call: its method, display key, rendered
// default arguments, source position and — under --load-configs — the
// key's current value.
type call struct {
	method     string // Get, Set or GetStrict, as written
	key        string // the display key
	defaults   string // the default arguments rendered, ", "-joined
	hasDefault bool
	file       string // the display filename, "" for appended config keys
	line       int
	col        int
	live       string
	liveSet    bool
}

// parseSource parses one Go file and records every package-qualified call
// to toxin.Get, toxin.Set or toxin.GetStrict. A file that cannot be read
// or parsed is skipped silently.
func parseSource(path, display string) []call {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil
	}

	var calls []call
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok || recv.Name != "toxin" {
			return true
		}
		var method string
		switch sel.Sel.Name {
		case "Get", "Set", "GetStrict":
			method = sel.Sel.Name
		default:
			return true
		}
		if len(ce.Args) == 0 {
			// No key argument: nothing to report for the call.
			return true
		}

		pos := fset.Position(ce.Pos())
		c := call{
			method: method,
			key:    displayKey(fset, src, ce.Args[0]),
			file:   display,
			line:   pos.Line,
			col:    pos.Column,
		}
		if len(ce.Args) > 1 {
			c.hasDefault = true
			parts := make([]string, 0, len(ce.Args)-1)
			for _, arg := range ce.Args[1:] {
				parts = append(parts, renderDefault(fset, arg))
			}
			c.defaults = strings.Join(parts, ", ")
		}
		calls = append(calls, c)
		return true
	})
	return calls
}

// displayKey renders the key argument for output: a string literal as its
// value, a bare identifier as the identifier, and any other non-literal
// expression as its source text wrapped in angle brackets.
func displayKey(fset *token.FileSet, src []byte, arg ast.Expr) string {
	switch k := arg.(type) {
	case *ast.BasicLit:
		if k.Kind == token.STRING {
			if s, err := strconv.Unquote(k.Value); err == nil {
				return s
			}
		}
		var b strings.Builder
		if format.Node(&b, fset, k) == nil {
			return b.String()
		}
		return k.Value
	case *ast.Ident:
		return k.Name
	default:
		return "<" + sourceText(fset, src, arg) + ">"
	}
}

// sourceText returns the source bytes of an expression.
func sourceText(fset *token.FileSet, src []byte, arg ast.Expr) string {
	start := fset.Position(arg.Pos()).Offset
	end := fset.Position(arg.End()).Offset
	if start < 0 || end > len(src) || start > end {
		return ""
	}
	return string(src[start:end])
}

// renderDefault renders one default argument in Go literal syntax. A
// literal — a basic literal, true/false/nil, a signed numeric literal or a
// composite literal — prints as go/printer renders it; anything else is an
// unreadable default and renders as "<unparsed>".
func renderDefault(fset *token.FileSet, arg ast.Expr) string {
	if !readableExpr(arg) {
		return "<unparsed>"
	}
	var b strings.Builder
	if format.Node(&b, fset, arg) != nil {
		return "<unparsed>"
	}
	return b.String()
}

// readableExpr reports whether arg is a literal expression renderable in
// Go literal syntax. Calls, selectors, binary expressions and named
// identifiers are not: they name a value rather than state one.
func readableExpr(arg ast.Expr) bool {
	switch a := arg.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return a.Name == "true" || a.Name == "false" || a.Name == "nil"
	case *ast.UnaryExpr:
		if a.Op != token.SUB && a.Op != token.ADD {
			return false
		}
		lit, ok := a.X.(*ast.BasicLit)
		return ok && (lit.Kind == token.INT || lit.Kind == token.FLOAT)
	case *ast.CompositeLit:
		return true
	default:
		return false
	}
}

// annotation is the source line prepended above a call under --source: a
// scanned call names its file and line, a key appended from the
// configuration under --load-configs says so instead.
func (c *call) annotation() string {
	if c.file == "" {
		return "# Loaded config"
	}
	return fmt.Sprintf("# %s, line %d", c.file, c.line)
}

// valueLine renders the call's output line in the selected view: the call
// as written under --view-call, the live namespace under --load-configs,
// and otherwise the namespace view "key = default".
func (c *call) valueLine(opts scanOptions) string {
	switch {
	case opts.viewCall:
		line := fmt.Sprintf("toxin.%s(%s", c.method, strconv.Quote(c.key))
		if c.hasDefault {
			line += ", " + c.defaults
		}
		return line + ")"
	case opts.loadConfigs:
		switch {
		case c.liveSet:
			return c.key + " = " + c.live
		case c.hasDefault:
			return c.key + " = " + c.defaults
		default:
			return c.key + " = <not set>"
		}
	default:
		if c.hasDefault {
			return c.key + " = " + c.defaults
		}
		return c.key + " = <not set>"
	}
}
