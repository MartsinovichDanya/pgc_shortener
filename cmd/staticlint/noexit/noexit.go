// Package noexit реализует анализатор, запрещающий прямой вызов os.Exit
// в функции main пакета main.
//
// Прямой вызов os.Exit внутри main прерывает программу мгновенно:
// не выполняются отложенные вызовы defer, не сбрасывается буферизованный
// вывод (bufio.Writer, логи), затрудняется тестирование. Правильный паттерн —
// вынести логику в func run() error/int, а сам вызов os.Exit спрятать в helper-функцию.
package noexit

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer запрещает вызывать os.Exit непосредственно в теле func main()
// пакета main.
var Analyzer = &analysis.Analyzer{
	Name:     "noexit",
	Doc:      "forbids direct calls to os.Exit inside func main of package main",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (interface{}, error) {
	// Анализатор имеет смысл только для пакета main.
	if pass.Pkg.Name() != "main" {
		return nil, nil
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// Собираем все объявления функций, чтобы потом проверить,
	// является ли enclosing-функция для вызова именно main.
	mainBodies := map[*ast.BlockStmt]bool{}
	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
		fd := n.(*ast.FuncDecl)
		if fd.Body != nil && fd.Recv == nil && fd.Name.Name == "main" {
			mainBodies[fd.Body] = true
		}
	})
	if len(mainBodies) == 0 {
		return nil, nil
	}

	// Ищем вызовы os.Exit внутри тела main (на любой глубине, включая замыкания).
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)

		// Проверка, что вызов лежит внутри тела main.
		if !inMainBody(call.Pos(), mainBodies) {
			return
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Exit" {
			return
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return
		}

		pkgName, ok := pass.TypesInfo.Uses[pkgIdent].(*types.PkgName)
		if !ok || pkgName.Imported().Path() != "os" {
			return
		}

		pass.Reportf(call.Pos(),
			"do not call os.Exit directly in func main; move it into a helper (e.g. exit(code)) or return an exit code from run()")
	})

	return nil, nil
}

// inMainBody возвращает true, если позиция p находится внутри тела
// одной из функций main, собранных в mainBodies.
func inMainBody(p token.Pos, mainBodies map[*ast.BlockStmt]bool) bool {
	for body := range mainBodies {
		if body.Pos() <= p && p < body.End() {
			return true
		}
	}
	return false
}
