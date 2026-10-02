package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestSpotShortSharedOwnershipGateIsWiredBeforeRegistration(t *testing.T) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "symbol_manager.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var installed, registered token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		receiver, _ := selector.X.(*ast.Ident)
		if receiver != nil && receiver.Name == "spotShortStrategy" && selector.Sel.Name == "SetOpeningGate" {
			if len(call.Args) != 1 {
				t.Fatal("missing shared gate argument")
			}
			gateCall, ok := call.Args[0].(*ast.CallExpr)
			if !ok {
				t.Fatal("gate must come from the startup position manager")
			}
			gateSelector, ok := gateCall.Fun.(*ast.SelectorExpr)
			if !ok || gateSelector.Sel.Name != "OpeningGate" {
				t.Fatal("wallet and executor must share the runtime gate")
			}
			manager, _ := gateSelector.X.(*ast.Ident)
			if manager == nil || manager.Name != "superPositionManager" {
				t.Fatal("wallet gate belongs to a different manager")
			}
			installed = call.Pos()
		}
		if selector.Sel.Name == "RegisterStrategy" {
			for _, arg := range call.Args {
				if identifier, ok := arg.(*ast.Ident); ok && identifier.Name == "spotShortStrategy" {
					registered = call.Pos()
				}
			}
		}
		return true
	})
	if installed == token.NoPos || registered == token.NoPos || installed >= registered {
		t.Fatal("SpotShort lacks shared wallet ownership gate wiring before registration")
	}
}
