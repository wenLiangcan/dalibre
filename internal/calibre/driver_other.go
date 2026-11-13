//go:build !darwin

package calibre

import (
	"database/sql/driver"

	"modernc.org/sqlite"
)

const libraryDriver = "sqlite"

func init() {
	for _, f := range sqlFuncs {
		if f.scalar != nil {
			fn := func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) { return f.scalar(args), nil }
			if f.deterministic {
				sqlite.MustRegisterDeterministicScalarFunction(f.name, f.nargs, fn)
			} else {
				sqlite.MustRegisterScalarFunction(f.name, f.nargs, fn)
			}
			continue
		}
		sqlite.MustRegisterFunction(f.name, &sqlite.FunctionImpl{
			NArgs:         f.nargs,
			Deterministic: f.deterministic,
			MakeAggregate: func(sqlite.FunctionContext) (sqlite.AggregateFunction, error) {
				return moderncAgg{f.aggregate()}, nil
			},
		})
	}
}

// moderncAgg adapts concatAgg to modernc's aggregate interface.
type moderncAgg struct{ *concatAgg }

func (a moderncAgg) Step(_ *sqlite.FunctionContext, args []driver.Value) error {
	a.add(args)
	return nil
}
func (a moderncAgg) WindowInverse(*sqlite.FunctionContext, []driver.Value) error { return nil }
func (a moderncAgg) WindowValue(*sqlite.FunctionContext) (driver.Value, error) {
	return a.value(), nil
}
func (a moderncAgg) Final(*sqlite.FunctionContext) {}
