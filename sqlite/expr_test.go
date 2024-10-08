package sqlite

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/stretchr/testify/assert"
)

type testExprData struct {
	expr string
	sql  string
	args []any
}

func Test_ExprBasic(t *testing.T) {
	testCases := []testExprData{
		{
			"hello",
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.msg", "*hello*"},
		},
		{
			"hello*",
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.msg", "hello*"},
		},
		{
			"hello world",
			"json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?",
			[]any{"$.msg", "*hello*", "$.msg", "*world*"},
		},
		{
			"hello* *world",
			"json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?",
			[]any{"$.msg", "hello*", "$.msg", "*world"},
		},
		{
			`"hello world"`,
			"json_extract(e.content, ?) = ?",
			[]any{"$.msg", "hello world"},
		},
		{
			`"hello world*"`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.msg", "hello world*"},
		},
		{
			`"*hello*world*"`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.msg", "*hello*world*"},
		},
		{
			`field:hello`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", "*hello*"},
		},
		{
			"field:hello*",
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", "hello*"},
		},
		{
			"field:hello world",
			"json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", "*hello*", "$.msg", "*world*"},
		},
		{
			"field:hello* *world",
			"json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", "hello*", "$.msg", "*world"},
		},
		{
			`field:"hello world"`,
			"json_extract(e.content, ?) = ?",
			[]any{"$.field", "hello world"},
		},
		{
			`field:"hello world*"`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", "hello world*"},
		},
		{
			`field:"*hello*world*"`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", "*hello*world*"},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.expr, func(t *testing.T) {
			runExprTest(t, tt)
		})
	}
}

// Numerical values
func Test_ExprNumerical(t *testing.T) {
	testCases := []testExprData{
		{
			`field:99`,
			`CAST(json_extract(e.content, ?) AS NUMERIC) = ?`,
			[]any{"$.field", float64(99)},
		},
		{
			`field:>99`,
			`CAST(json_extract(e.content, ?) AS NUMERIC) > ?`,
			[]any{"$.field", float64(99)},
		},
		{
			`field:<99`,
			`CAST(json_extract(e.content, ?) AS NUMERIC) < ?`,
			[]any{"$.field", float64(99)},
		},
		{
			`field:>=99`,
			`CAST(json_extract(e.content, ?) AS NUMERIC) >= ?`,
			[]any{"$.field", float64(99)},
		},
		{
			`field:<=99`,
			`CAST(json_extract(e.content, ?) AS NUMERIC) <= ?`,
			[]any{"$.field", float64(99)},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.expr, func(t *testing.T) {
			runExprTest(t, tt)
		})
	}
}

func Test_ExprArray(t *testing.T) {
	testCases := []testExprData{
		{
			`[hello world]`,
			"json_extract(e.content, ?) IN (?, ?)",
			[]any{"$.msg", "hello", "world"},
		},
		{
			`[hello "beautiful world"]`,
			"json_extract(e.content, ?) IN (?, ?)",
			[]any{"$.msg", "hello", "beautiful world"},
		},
		{
			`field:[hello world]`,
			"json_extract(e.content, ?) IN (?, ?)",
			[]any{"$.field", "hello", "world"},
		},
		{
			`field:[hello "beautiful world"]`,
			"json_extract(e.content, ?) IN (?, ?)",
			[]any{"$.field", "hello", "beautiful world"},
		},
		{
			`field:[400 TO 499]`,
			`CAST(json_extract(e.content, ?) AS NUMERIC) BETWEEN ? AND ?`,
			[]any{"$.field", float64(400), float64(499)},
		},
		{
			`field:[100 200 300]`,
			`CAST(json_extract(e.content, ?) AS NUMERIC) IN (?, ?, ?)`,
			[]any{"$.field", float64(100), float64(200), float64(300)},
		},
		{
			`field:[100 hello "beautiful world" 200 300]`,
			`(CAST(json_extract(e.content, ?) AS NUMERIC) IN (?, ?, ?) OR json_extract(e.content, ?) IN (?, ?))`,
			[]any{"$.field", float64(100), float64(200), float64(300), "$.field", "hello", "beautiful world"},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.expr, func(t *testing.T) {
			runExprTest(t, tt)
		})
	}
}

// Boolean Operators
func Test_ExprBoolean(t *testing.T) {
	testCases := []testExprData{
		{
			"hello AND world",
			`json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?`,
			[]any{"$.msg", "*hello*", "$.msg", "*world*"},
		},
		{
			"hello AND beautiful AND world",
			`json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?`,
			[]any{"$.msg", "*hello*", "$.msg", "*beautiful*", "$.msg", "*world*"},
		},
		{
			"hello OR world",
			`json_extract(e.content, ?) GLOB ? OR json_extract(e.content, ?) GLOB ?`,
			[]any{"$.msg", "*hello*", "$.msg", "*world*"},
		},
		{
			"field:hello AND world",
			`json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?`,
			[]any{"$.field", "*hello*", "$.msg", "*world*"},
		},
		{
			"field:hello AND beautiful AND field:world",
			`json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?`,
			[]any{"$.field", "*hello*", "$.msg", "*beautiful*", "$.field", "*world*"},
		},
		{
			"field:hello OR world",
			`json_extract(e.content, ?) GLOB ? OR json_extract(e.content, ?) GLOB ?`,
			[]any{"$.field", "*hello*", "$.msg", "*world*"},
		},
		{
			"hello AND (beautiful OR world)",
			`json_extract(e.content, ?) GLOB ? AND (json_extract(e.content, ?) GLOB ? OR json_extract(e.content, ?) GLOB ?)`,
			[]any{"$.msg", "*hello*", "$.msg", "*beautiful*", "$.msg", "*world*"},
		},
		{
			"hello AND (beautiful AND world)",
			`json_extract(e.content, ?) GLOB ? AND (json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?)`,
			[]any{"$.msg", "*hello*", "$.msg", "*beautiful*", "$.msg", "*world*"},
		},
		{
			"field:hello AND (beautiful AND field:99)",
			`json_extract(e.content, ?) GLOB ? AND (json_extract(e.content, ?) GLOB ? AND CAST(json_extract(e.content, ?) AS NUMERIC) = ?)`,
			[]any{"$.field", "*hello*", "$.msg", "*beautiful*", "$.field", float64(99)},
		},
		{
			`(field:hello* OR world*) AND (field:[hello "beautiful world"] OR (field:99 AND field:[100 200 300]) OR field:[400 TO 499])`,
			`(json_extract(e.content, ?) GLOB ? OR json_extract(e.content, ?) GLOB ?) AND ` +
				`( json_extract(e.content, ?) IN (?,?) OR ` +
				`   (CAST(json_extract(e.content, ?) AS NUMERIC) = ? AND CAST(json_extract(e.content, ?) AS NUMERIC) IN (?,?,?)) OR` +
				`   CAST(json_extract(e.content, ?) AS NUMERIC) BETWEEN ? AND ?` +
				`)`,
			[]any{
				"$.field", "hello*", "$.msg", "world*",
				"$.field", "hello", "beautiful world",
				"$.field", float64(99), "$.field", float64(100), float64(200), float64(300),
				"$.field", float64(400), float64(499),
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.expr, func(t *testing.T) {
			runExprTest(t, tt)
		})
	}
}

func Test_ExprEscape(t *testing.T) {
	testCases := []testExprData{
		{
			`hell\"o`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.msg", `*hell"o*`},
		},
		{
			`"hello \" world"`,
			"json_extract(e.content, ?) = ?",
			[]any{"$.msg", `hello " world`},
		},
		{
			`"hello \" world*"`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.msg", `hello " world*`},
		},
		{
			`field:hell\"o`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", `*hell"o*`},
		},
		{
			`field:"hello \" world"`,
			"json_extract(e.content, ?) = ?",
			[]any{"$.field", `hello " world`},
		},
		{
			`field:"hello \" world*"`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", `hello " world*`},
		},
		{
			`field:"hello [beautiful] world*"`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", `hello [beautiful] world*`},
		},
		{
			`field:he\[ll]\"o`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.field", `*he[ll]"o*`},
		},
		{
			`field:[hell\"o "beautiful \" world"]`,
			"json_extract(e.content, ?) IN (?, ?)",
			[]any{"$.field", `hell"o`, `beautiful " world`},
		},
		{
			`field:[hell\"o world\]]`,
			"json_extract(e.content, ?) IN (?, ?)",
			[]any{"$.field", `hell"o`, `world]`},
		},
		{
			`path:c\:/dev/projects/*`,
			"json_extract(e.content, ?) GLOB ?",
			[]any{"$.path", `c:/dev/projects/*`},
		},
		{
			`(hell\"o AND \"world)`,
			"(json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?)",
			[]any{"$.msg", `*hell"o*`, "$.msg", `*"world*`},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.expr, func(t *testing.T) {
			runExprTest(t, tt)
		})
	}
}

func Test_ExprIncomplete(t *testing.T) {
	testCases := []testExprData{
		{
			`"hello \" world`,
			"json_extract(e.content, ?) = ?",
			[]any{"$.msg", `hello " world`},
		},
		{
			`field:[hell\"o "beautiful \" world"`,
			"json_extract(e.content, ?) IN (?, ?)",
			[]any{"$.field", `hell"o`, `beautiful " world`},
		},
		{
			`field:[hell\"o world\]`,
			"json_extract(e.content, ?) IN (?, ?)",
			[]any{"$.field", `hell"o`, `world]`},
		},
		{
			`field:[]`,
			"",
			[]any{},
		},
		{
			`field:[     ]`,
			"",
			[]any{},
		},
		{
			`(hell\"o AND \"world`,
			"(json_extract(e.content, ?) GLOB ? AND json_extract(e.content, ?) GLOB ?)",
			[]any{"$.msg", `*hell"o*`, "$.msg", `*"world*`},
		},
		{
			`(field:hello* OR world*) AND (field:[hello "beautiful world"] OR (field:99 AND field:[100 200 300`,
			`(json_extract(e.content, ?) GLOB ? OR json_extract(e.content, ?) GLOB ?) AND ` +
				`( json_extract(e.content, ?) IN (?,?) OR ` +
				`   (CAST(json_extract(e.content, ?) AS NUMERIC) = ? AND CAST(json_extract(e.content, ?) AS NUMERIC) IN (?,?,?))` +
				`)`,
			[]any{
				"$.field", "hello*", "$.msg", "world*",
				"$.field", "hello", "beautiful world",
				"$.field", float64(99), "$.field", float64(100), float64(200), float64(300),
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.expr, func(t *testing.T) {
			runExprTest(t, tt)
		})
	}
}

func runExprTest(t *testing.T, tt testExprData) {
	compiled, err := ExpBuilderFn(tt.expr)
	assert.NoError(t, err)

	actual := compiled.Sql
	expected := tt.sql

	a := strings.Join(strings.Fields(actual), "")
	b := strings.Join(strings.Fields(expected), "")
	if !reflect.DeepEqual(a, b) {

		diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(reflect.ValueOf(expected).String()),
			B:        difflib.SplitLines(reflect.ValueOf(actual).String()),
			FromFile: "Expected",
			FromDate: "",
			ToFile:   "Actual",
			ToDate:   "",
			Context:  1,
		})

		assert.Fail(t, fmt.Sprintf("Not equal: \n"+
			"expected: %s\n"+
			"actual  : %s\n\n"+
			"Diff: %s\n", expected, actual, diff))
		return
	}
	assert.Equal(t, tt.args, compiled.Args)
}
