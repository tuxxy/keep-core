package libp2p

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	addrutil "github.com/libp2p/go-addr-util"
	ma "github.com/multiformats/go-multiaddr"
)

// TestB17aEmptyListenAddressesKeepBaseListenSet (unit; opens no listener).
//
// Invariant (decision D35 (a): "empty means every interface, as base", so
// legacy is unchanged, shared rule 5): with no ListenAddresses configured,
// the provider listens on base's set, every interface address on the
// configured port, exactly as getListenAddrs computes it.
//
// Checking the listen set by listening would open LAN listeners, so this
// test checks it without any socket, in two parts:
//
//   - getListenAddrs(port), base's pure helper, still returns every
//     addrutil.InterfaceAddresses() address on that port (so a fix cannot
//     make it loopback-only);
//   - in libp2p.go, the address list handed to libp2p.ListenAddrs is
//     assigned from getListenAddrs(port), and every other assignment to it
//     sits in a branch taken only when a configured list is non-empty: the
//     then-branch of `len(x) > 0`, `len(x) != 0` or `0 < len(x)`, or the
//     else-branch of `len(x) == 0`.
//
// The second part is a source check of the fix's shape. A correct fix of
// another shape, for example a new selection helper, fails it and needs a
// deliberate update of this test, never a weakening of the base default.
//
// Refs tuxxy/keep-core#37
func TestB17aEmptyListenAddressesKeepBaseListenSet(t *testing.T) {
	const port = 3919
	interfaces, err := addrutil.InterfaceAddresses()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, a := range interfaces {
		want = append(want, a.Encapsulate(ma.StringCast(fmt.Sprintf("/tcp/%d", port))).String())
	}
	got, err := getListenAddrs(port)
	if err != nil {
		t.Fatal(err)
	}
	var gotS []string
	for _, a := range got {
		gotS = append(gotS, a.String())
	}
	if !reflect.DeepEqual(gotS, want) {
		t.Errorf("K09-20: getListenAddrs(%d) = %v; base listens on every interface address %v", port, gotS, want)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "libp2p.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		listened := listenAddrsArgument(fn.Body)
		if listened == "" {
			continue
		}
		checked++
		fromBase := 0
		for _, a := range assignmentsTo(fn.Body, listened) {
			if call, ok := a.value.(*ast.CallExpr); ok && callName(call) == "getListenAddrs" {
				if a.nonEmptyOnly {
					t.Errorf("K09-20: %s assigns %s from getListenAddrs only when a configured list is non-empty (%s)", fn.Name.Name, listened, fset.Position(a.pos))
				}
				fromBase++
				continue
			}
			if !a.nonEmptyOnly {
				t.Errorf("K09-20: %s assigns the listen set %s at %s on a path where no listen address is configured; empty ListenAddresses must keep base's getListenAddrs(port) set", fn.Name.Name, listened, fset.Position(a.pos))
			}
		}
		if fromBase == 0 {
			t.Errorf("K09-20: %s no longer takes its listen set %s from getListenAddrs(port), base's every-interface set", fn.Name.Name, listened)
		}
	}
	if checked == 0 {
		t.Fatalf("K09-20: no function in libp2p.go calls libp2p.ListenAddrs(<list>...); the source check needs updating")
	}
}

// listenAddrsArgument is the identifier passed as libp2p.ListenAddrs(x...)
// in body, or "".
func listenAddrsArgument(body *ast.BlockStmt) string {
	name := ""
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || callName(call) != "ListenAddrs" || len(call.Args) != 1 {
			return true
		}
		if id, ok := call.Args[0].(*ast.Ident); ok {
			name = id.Name
		}
		return true
	})
	return name
}

func callName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

type assignment struct {
	value        ast.Expr
	pos          token.Pos
	nonEmptyOnly bool
}

// nonEmptyCondition reports whether cond is true exactly when some len(x) is
// non-zero (then-branch), or exactly when it is zero (else-branch).
func nonEmptyCondition(cond ast.Expr) (thenNonEmpty, elseNonEmpty bool) {
	b, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return false, false
	}
	isLen := func(e ast.Expr) bool {
		c, ok := e.(*ast.CallExpr)
		return ok && callName(c) == "len"
	}
	isZero := func(e ast.Expr) bool {
		l, ok := e.(*ast.BasicLit)
		return ok && l.Value == "0"
	}
	switch {
	case isLen(b.X) && isZero(b.Y) && (b.Op == token.GTR || b.Op == token.NEQ),
		isZero(b.X) && isLen(b.Y) && (b.Op == token.LSS || b.Op == token.NEQ):
		return true, false
	case isLen(b.X) && isZero(b.Y) && b.Op == token.EQL,
		isZero(b.X) && isLen(b.Y) && b.Op == token.EQL:
		return false, true
	}
	return false, false
}

// assignmentsTo lists every assignment to name in body, with whether it is
// reachable only when a configured list is non-empty.
func assignmentsTo(body *ast.BlockStmt, name string) []assignment {
	var out []assignment
	var walk func(n ast.Node, nonEmpty bool)
	walk = func(n ast.Node, nonEmpty bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch s := m.(type) {
			case *ast.IfStmt:
				thenNE, elseNE := nonEmptyCondition(s.Cond)
				if s.Init != nil {
					walk(s.Init, nonEmpty)
				}
				walk(s.Body, nonEmpty || thenNE)
				if s.Else != nil {
					walk(s.Else, nonEmpty || elseNE)
				}
				return false
			case *ast.AssignStmt:
				for i, lhs := range s.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || id.Name != name {
						continue
					}
					var value ast.Expr
					if len(s.Rhs) == len(s.Lhs) {
						value = s.Rhs[i]
					} else if len(s.Rhs) == 1 {
						value = s.Rhs[0]
					}
					out = append(out, assignment{value: value, pos: s.Pos(), nonEmptyOnly: nonEmpty})
				}
			case *ast.FuncLit:
				return false
			}
			return true
		})
	}
	walk(body, false)
	return out
}
