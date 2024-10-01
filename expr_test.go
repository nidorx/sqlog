package sqlog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	testExprBuilderFn = NewExprBuilder(func(expression string) (ExprBuilder[[]any], string) {
		return &testExprBuilder{parts: []any{}}, expression
	})
)

type testExprBuilder struct {
	parts []any
}

func (s *testExprBuilder) Build() []any {
	return s.parts
}

func (s *testExprBuilder) GroupStart() {
	s.parts = append(s.parts, "(")
}

func (s *testExprBuilder) GroupEnd() {
	s.parts = append(s.parts, ")")
}

func (s *testExprBuilder) Operator(op string) {
	s.parts = append(s.parts, op)
}

func (s *testExprBuilder) Text(field, term string, isSequence, isWildcard bool) {
	if isSequence {
		if isWildcard {
			s.parts = append(s.parts, field, "LIKE SEQ", term)
		} else {
			s.parts = append(s.parts, field, "EQUAL", term)
		}
	} else {
		s.parts = append(s.parts, field, "LIKE", term)
	}
}

func (s *testExprBuilder) TextIn(field string, values []string) {
	s.parts = append(s.parts, field, "IN")
	for _, v := range values {
		s.parts = append(s.parts, v)
	}
}

func (s *testExprBuilder) Number(field, condition string, value float64) {
	s.parts = append(s.parts, field, condition, value)
}

func (s *testExprBuilder) Between(field string, x, y float64) {
	s.parts = append(s.parts, field, "BETWEEN", x, y)
}

func (s *testExprBuilder) NumberIn(field string, values []float64) {
	s.parts = append(s.parts, field)
	s.parts = append(s.parts, "IN NUMERIC")
	for _, v := range values {
		s.parts = append(s.parts, v)
	}
}

type testExprData struct {
	expr  string
	parts []any
}

func Test_ExprBasic(t *testing.T) {
	testCases := []testExprData{
		{
			"hello",
			[]any{"msg", "LIKE", "hello"},
		},
		{
			"hello*",
			[]any{"msg", "LIKE", "hello*"},
		},
		{
			"hello world",
			[]any{"msg", "LIKE", "hello", "OR", "msg", "LIKE", "world"},
		},
		{
			"hello* *world",
			[]any{"msg", "LIKE", "hello*", "OR", "msg", "LIKE", "*world"},
		},
		{
			`"hello world"`,
			[]any{"msg", "EQUAL", "hello world"},
		},
		{
			`"hello world*"`,
			[]any{"msg", "LIKE SEQ", "hello world*"},
		},
		{
			`"*hello*world*"`,
			[]any{"msg", "LIKE SEQ", "*hello*world*"},
		},
		{
			`field:hello`,
			[]any{"field", "LIKE", "hello"},
		},
		{
			"field:hello*",
			[]any{"field", "LIKE", "hello*"},
		},
		{
			"field:hello world",
			[]any{"field", "LIKE", "hello", "OR", "msg", "LIKE", "world"},
		},
		{
			"field:hello* *world",
			[]any{"field", "LIKE", "hello*", "OR", "msg", "LIKE", "*world"},
		},
		{
			`field:"hello world"`,
			[]any{"field", "EQUAL", "hello world"},
		},
		{
			`field:"hello world*"`,
			[]any{"field", "LIKE SEQ", "hello world*"},
		},
		{
			`field:"*hello*world*"`,
			[]any{"field", "LIKE SEQ", "*hello*world*"},
		},
	}
	for _, tt := range testCases {
		runExprTest(t, tt)
	}
}

// Numerical values
func Test_ExprNumerical(t *testing.T) {
	testCases := []testExprData{
		{
			`field:99`,
			[]any{"field", "=", float64(99)},
		},
		{
			`field:>99`,
			[]any{"field", ">", float64(99)},
		},
		{
			`field:<99`,
			[]any{"field", "<", float64(99)},
		},
		{
			`field:>=99`,
			[]any{"field", ">=", float64(99)},
		},
		{
			`field:<=99`,
			[]any{"field", "<=", float64(99)},
		},
	}
	for _, tt := range testCases {
		runExprTest(t, tt)
	}
}

func Test_ExprArray(t *testing.T) {
	testCases := []testExprData{
		{
			`[hello world]`,
			[]any{"msg", "IN", "hello", "world"},
		},
		{
			`[hello "beautiful world"]`,
			[]any{"msg", "IN", "hello", "beautiful world"},
		},
		{
			`field:[hello world]`,
			[]any{"field", "IN", "hello", "world"},
		},
		{
			`field:[hello "beautiful world"]`,
			[]any{"field", "IN", "hello", "beautiful world"},
		},
		{
			`field:[400 TO 499]`,
			[]any{"field", "BETWEEN", float64(400), float64(499)},
		},
		{
			`field:[100 200 300]`,
			[]any{"field", "IN NUMERIC", float64(100), float64(200), float64(300)},
		},
		{
			`field:[100 hello "beautiful world" 200 300]`,
			[]any{"(", "field", "IN NUMERIC", float64(100), float64(200), float64(300), "OR", "field", "IN", "hello", "beautiful world", ")"},
		},
	}
	for _, tt := range testCases {
		runExprTest(t, tt)
	}
}

// Boolean Operators
func Test_ExprBoolean(t *testing.T) {
	testCases := []testExprData{
		{
			"hello AND world",
			[]any{"msg", "LIKE", "hello", "AND", "msg", "LIKE", "world"},
		},
		{
			"hello AND beautiful AND world",
			[]any{"msg", "LIKE", "hello", "AND", "msg", "LIKE", "beautiful", "AND", "msg", "LIKE", "world"},
		},
		{
			"hello OR world",
			[]any{"msg", "LIKE", "hello", "OR", "msg", "LIKE", "world"},
		},
		{
			"field:hello AND world",
			[]any{"field", "LIKE", "hello", "AND", "msg", "LIKE", "world"},
		},
		{
			"field:hello AND beautiful AND field:world",
			[]any{"field", "LIKE", "hello", "AND", "msg", "LIKE", "beautiful", "AND", "field", "LIKE", "world"},
		},
		{
			"field:hello OR world",
			[]any{"field", "LIKE", "hello", "OR", "msg", "LIKE", "world"},
		},
		{
			"hello AND (beautiful world)",
			[]any{"msg", "LIKE", "hello", "AND", "(", "msg", "LIKE", "beautiful", "OR", "msg", "LIKE", "world", ")"},
		},
		{
			"hello AND (beautiful AND world)",
			[]any{"msg", "LIKE", "hello", "AND", "(", "msg", "LIKE", "beautiful", "AND", "msg", "LIKE", "world", ")"},
		},
		{
			"field:hello AND (beautiful AND field:99)",
			[]any{"field", "LIKE", "hello", "AND", "(", "msg", "LIKE", "beautiful", "AND", "field", "=", float64(99), ")"},
		},
		{
			`(field:hello* OR world*) AND (field:[hello "beautiful world"] OR (field:99 AND field:[100 200 300]) OR field:[400 TO 499])`,
			[]any{
				"(", "field", "LIKE", "hello*", "OR", "msg", "LIKE", "world*", ")", "AND",
				"(",
				"field", "IN", "hello", "beautiful world", "OR",
				"(", "field", "=", float64(99), "AND", "field", "IN NUMERIC", float64(100), float64(200), float64(300), ")", "OR",
				"field", "BETWEEN", float64(400), float64(499),
				")",
			},
		},
	}
	for _, tt := range testCases {
		runExprTest(t, tt)
	}
}

func Test_ExprEscape(t *testing.T) {
	testCases := []testExprData{
		{
			`hell\"o`,
			[]any{"msg", "LIKE", `hell"o`},
		},
		{
			`"hello \" world"`,
			[]any{"msg", "EQUAL", `hello " world`},
		},
		{
			`"hello \" world*"`,
			[]any{"msg", "LIKE SEQ", `hello " world*`},
		},
		{
			`field:hell\"o`,
			[]any{"field", "LIKE", `hell"o`},
		},
		{
			`field:"hello \" world"`,
			[]any{"field", "EQUAL", `hello " world`},
		},
		{
			`field:"hello \" world*"`,
			[]any{"field", "LIKE SEQ", `hello " world*`},
		},
		{
			`field:"hello [beautiful] world*"`,
			[]any{"field", "LIKE SEQ", `hello [beautiful] world*`},
		},
		{
			`field:he\[ll]\"o`,
			[]any{"field", "LIKE", `he[ll]"o`},
		},
		{
			`field:[hell\"o "beautiful \" world"]`,
			[]any{"field", "IN", `hell"o`, `beautiful " world`},
		},
		{
			`field:[hell\"o world\]]`,
			[]any{"field", "IN", `hell"o`, `world]`},
		},
		{
			`path:c\:/dev/projects/*`,
			[]any{"path", "LIKE", `c:/dev/projects/*`},
		},
		{
			`(hell\"o AND \"world)`,
			[]any{"(", "msg", "LIKE", `hell"o`, "AND", "msg", "LIKE", `"world`, ")"},
		},
	}
	for _, tt := range testCases {
		runExprTest(t, tt)
	}
}

func Test_ExprIncomplete(t *testing.T) {
	testCases := []testExprData{
		{
			`"hello \" world`,
			[]any{"msg", "EQUAL", `hello " world`},
		},
		{
			`field:[hell\"o "beautiful \" world"`,
			[]any{"field", "IN", `hell"o`, `beautiful " world`},
		},
		{
			`field:[hell\"o world\]`,
			[]any{"field", "IN", `hell"o`, `world]`},
		},
		{
			`field:[]`,
			[]any{},
		},
		{
			`field:[     ]`,
			[]any{},
		},
		{
			`(hell\"o AND \"world`,
			[]any{"(", "msg", "LIKE", `hell"o`, "AND", "msg", "LIKE", `"world`, ")"},
		},
		{
			`(field:hello* OR world*) AND (field:[hello "beautiful world"] OR (field:99 AND field:[100 200 300`,
			[]any{
				"(", "field", "LIKE", "hello*", "OR", "msg", "LIKE", "world*", ")", "AND",
				"(",
				"field", "IN", "hello", "beautiful world", "OR",
				"(", "field", "=", float64(99), "AND", "field", "IN NUMERIC", float64(100), float64(200), float64(300), ")",
				")",
			},
		},
	}
	for _, tt := range testCases {
		runExprTest(t, tt)
	}
}

func runExprTest(t *testing.T, tt testExprData) {
	compiled, err := testExprBuilderFn(tt.expr)
	assert.NoError(t, err)
	assert.Equal(t, tt.parts, compiled, "exp=%s", tt.expr)
}
