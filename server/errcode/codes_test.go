package errcode

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestCodesUnique asserts that every code declared in AllCodes is distinct.
// Two log call sites must never share an identifier.
func TestCodesUnique(t *testing.T) {
	seen := make(map[int]int, len(AllCodes))
	for i, code := range AllCodes {
		if prev, ok := seen[code]; ok {
			t.Errorf("duplicate code %d at AllCodes[%d] (also at index %d)", code, i, prev)
		}
		seen[code] = i
	}
}

// TestAllCodesComplete asserts that every integer constant declared in
// codes.go is also present in the AllCodes slice. This guards against an
// author adding a new constant but forgetting to append it to AllCodes,
// which would silently escape TestCodesUnique's duplicate check.
func TestAllCodesComplete(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "codes.go", nil, 0)
	if err != nil {
		t.Fatalf("parse codes.go: %v", err)
	}

	declared := make(map[string]bool)
	inAllCodes := make(map[string]bool)

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch gd.Tok {
		case token.CONST:
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					declared[name.Name] = true
				}
			}
		case token.VAR:
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if name.Name != "AllCodes" || i >= len(vs.Values) {
						continue
					}
					cl, ok := vs.Values[i].(*ast.CompositeLit)
					if !ok {
						continue
					}
					for _, elt := range cl.Elts {
						ident, ok := elt.(*ast.Ident)
						if !ok {
							continue
						}
						inAllCodes[ident.Name] = true
					}
				}
			}
		}
	}

	if len(declared) == 0 {
		t.Fatal("no const declarations found in codes.go; parser misconfigured?")
	}
	if len(inAllCodes) == 0 {
		t.Fatal("AllCodes slice literal not found in codes.go; parser misconfigured?")
	}

	for name := range declared {
		if !inAllCodes[name] {
			t.Errorf("constant %s is declared but missing from AllCodes", name)
		}
	}
	for name := range inAllCodes {
		if !declared[name] {
			t.Errorf("AllCodes references %s but it is not declared as a constant", name)
		}
	}
}
