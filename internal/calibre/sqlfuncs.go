package calibre

import (
	"crypto/rand"
	"database/sql/driver"
	"fmt"
	"sort"
	"strings"
)

// sqlFunc is one of the functions calibre registers on its connection; its
// triggers and views call them, so a write to books or series fails
// without them. Each driver registers this one table.
type sqlFunc struct {
	name          string
	nargs         int32
	deterministic bool
	scalar        func(args []driver.Value) driver.Value // nil for aggregates
	aggregate     func() *concatAgg
}

var sqlFuncs = []sqlFunc{
	{name: "title_sort", nargs: 1, deterministic: true, scalar: func(args []driver.Value) driver.Value {
		return TitleSort(asString(args[0]))
	}},
	{name: "uuid4", scalar: func([]driver.Value) driver.Value { return newUUID() }},
	// books_list_filter is calibre's in-memory search restriction; outside
	// calibre every book passes.
	{name: "books_list_filter", nargs: 1, scalar: func([]driver.Value) driver.Value { return int64(1) }},
	{name: "concat", nargs: 1, aggregate: func() *concatAgg { return &concatAgg{sep: ","} }},
	{name: "sortconcat", nargs: 2, aggregate: func() *concatAgg { return &concatAgg{sep: ",", sorted: true} }},
	{name: "sortconcat_bar", nargs: 2, aggregate: func() *concatAgg { return &concatAgg{sep: "|", sorted: true} }},
	{name: "sortconcat_amper", nargs: 2, aggregate: func() *concatAgg { return &concatAgg{sep: "&", sorted: true} }},
	{name: "identifiers_concat", nargs: 2, aggregate: func() *concatAgg { return &concatAgg{sep: ",", pair: true} }},
}

func asString(v driver.Value) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type concatItem struct {
	key int64
	val string
}

// concatAgg implements calibre's concat, sortconcat* and identifiers_concat.
type concatAgg struct {
	sep    string
	sorted bool // first argument is an ordering key
	pair   bool // "type:val"
	items  []concatItem
}

func (a *concatAgg) add(args []driver.Value) {
	switch {
	case a.pair:
		a.items = append(a.items, concatItem{val: asString(args[0]) + ":" + asString(args[1])})
	case a.sorted:
		k, _ := args[0].(int64)
		a.items = append(a.items, concatItem{key: k, val: asString(args[1])})
	default:
		a.items = append(a.items, concatItem{val: asString(args[0])})
	}
}

// value is the joined result, or nil when no rows were added.
func (a *concatAgg) value() driver.Value {
	if len(a.items) == 0 {
		return nil
	}
	if a.sorted {
		sort.SliceStable(a.items, func(i, j int) bool { return a.items[i].key < a.items[j].key })
	}
	parts := make([]string, len(a.items))
	for i, it := range a.items {
		parts[i] = it.val
	}
	return strings.Join(parts, a.sep)
}
