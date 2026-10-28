// Command staticlint — multichecker, запускающий набор статических анализаторов:
// стандартные passes из golang.org/x/tools (go vet), все SA-анализаторы
// staticcheck.io, классы S/ST/QF, два внешних публичных анализатора
// (fieldalignment, nilness) и собственный анализатор noexit.
//
// Запуск из корня модуля:
//
//	go run ./cmd/staticlint ./...
//
// или:
//
//	go build -o bin/staticlint ./cmd/staticlint && ./bin/staticlint ./...
//
// Полезные флаги multichecker: -flags (список анализаторов), -json,
// -<имя-анализатора>=false для отключения конкретного анализатора.
package main

import (
	"honnef.co/go/tools/quickfix"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/multichecker"
	"golang.org/x/tools/go/analysis/passes/asmdecl"
	"golang.org/x/tools/go/analysis/passes/assign"
	"golang.org/x/tools/go/analysis/passes/atomic"
	"golang.org/x/tools/go/analysis/passes/atomicalign"
	"golang.org/x/tools/go/analysis/passes/bools"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/analysis/passes/buildtag"
	"golang.org/x/tools/go/analysis/passes/cgocall"
	"golang.org/x/tools/go/analysis/passes/composite"
	"golang.org/x/tools/go/analysis/passes/copylock"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/deepequalerrors"
	"golang.org/x/tools/go/analysis/passes/defers"
	"golang.org/x/tools/go/analysis/passes/directive"
	"golang.org/x/tools/go/analysis/passes/errorsas"
	"golang.org/x/tools/go/analysis/passes/fieldalignment"
	"golang.org/x/tools/go/analysis/passes/findcall"
	"golang.org/x/tools/go/analysis/passes/framepointer"
	"golang.org/x/tools/go/analysis/passes/httpresponse"
	"golang.org/x/tools/go/analysis/passes/ifaceassert"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/analysis/passes/loopclosure"
	"golang.org/x/tools/go/analysis/passes/lostcancel"
	"golang.org/x/tools/go/analysis/passes/nilfunc"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/go/analysis/passes/reflectvaluecompare"
	"golang.org/x/tools/go/analysis/passes/shadow"
	"golang.org/x/tools/go/analysis/passes/shift"
	"golang.org/x/tools/go/analysis/passes/sigchanyzer"
	"golang.org/x/tools/go/analysis/passes/slog"
	"golang.org/x/tools/go/analysis/passes/sortslice"
	"golang.org/x/tools/go/analysis/passes/stdmethods"
	"golang.org/x/tools/go/analysis/passes/stringintconv"
	"golang.org/x/tools/go/analysis/passes/structtag"
	"golang.org/x/tools/go/analysis/passes/testinggoroutine"
	"golang.org/x/tools/go/analysis/passes/tests"
	"golang.org/x/tools/go/analysis/passes/timeformat"
	"golang.org/x/tools/go/analysis/passes/unmarshal"
	"golang.org/x/tools/go/analysis/passes/unreachable"
	"golang.org/x/tools/go/analysis/passes/unsafeptr"
	"golang.org/x/tools/go/analysis/passes/unusedresult"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"

	"github.com/MartsinovichDanya/pgc_shortener/cmd/staticlint/noexit"
)

// passAnalyzers — стандартные анализаторы golang.org/x/tools/go/analysis/passes
// (набор go vet). Полный список пакетов passes в x/tools v0.13.0; часть из них
// (buildssa, ctrlflow, inspect, pkgfact и т.п.) являются сервисными и нужны
// другим анализаторам как зависимости — их тоже безопасно включить.
var passAnalyzers = []*analysis.Analyzer{
	asmdecl.Analyzer, assign.Analyzer, atomic.Analyzer, atomicalign.Analyzer,
	bools.Analyzer, buildssa.Analyzer, buildtag.Analyzer, cgocall.Analyzer,
	composite.Analyzer, copylock.Analyzer, ctrlflow.Analyzer, deepequalerrors.Analyzer,
	defers.Analyzer, directive.Analyzer, errorsas.Analyzer, fieldalignment.Analyzer,
	findcall.Analyzer, framepointer.Analyzer, httpresponse.Analyzer, ifaceassert.Analyzer,
	inspect.Analyzer, loopclosure.Analyzer, lostcancel.Analyzer, nilfunc.Analyzer,
	nilness.Analyzer, printf.Analyzer, reflectvaluecompare.Analyzer, shadow.Analyzer,
	shift.Analyzer, sigchanyzer.Analyzer, slog.Analyzer, sortslice.Analyzer,
	stdmethods.Analyzer, stringintconv.Analyzer, structtag.Analyzer,
	testinggoroutine.Analyzer, tests.Analyzer, timeformat.Analyzer, unmarshal.Analyzer,
	unreachable.Analyzer, unsafeptr.Analyzer, unusedresult.Analyzer, unusedwrite.Analyzer,
}

func main() {
	all := append([]*analysis.Analyzer{}, passAnalyzers...)

	// Класс SA — ВСЕ анализаторы корректности staticcheck.io.
	for _, a := range staticcheck.Analyzers {
		all = append(all, a.Analyzer)
	}
	// Другие классы: S (упрощения), ST (стиль), QF (быстрые исправления).
	for _, a := range simple.Analyzers {
		all = append(all, a.Analyzer)
	}
	for _, a := range stylecheck.Analyzers {
		all = append(all, a.Analyzer)
	}
	for _, a := range quickfix.Analyzers {
		all = append(all, a.Analyzer)
	}

	// Собственный анализатор.
	all = append(all, noexit.Analyzer)

	multichecker.Main(all...)
}
