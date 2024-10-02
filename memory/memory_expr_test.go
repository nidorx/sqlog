package memory

import (
	"encoding/json"
	"sqlog"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testExpMemoryEntries = []*sqlog.Entry{}

func init() {
	for _, log := range testExpMemoryLogs {
		testExpMemoryEntries = append(testExpMemoryEntries, &sqlog.Entry{
			Content: []byte(log),
		})
	}
}

type testExprMemoryData struct {
	expr string
	ids  []int
}

// json entries
var testExpMemoryLogs = []string{
	`{"id":  1, "msg":"hello"}`,
	`{"id":  2, "msg":"Hello"}`,
	`{"id":  3, "msg":"world"}`,
	`{"id":  4, "msg":"World"}`,
	`{"id":  5, "msg":"beautiful"}`,
	`{"id":  6, "msg":"Beautiful"}`,
	`{"id":  7, "msg":"hello world"}`,
	`{"id":  8, "msg":"hello world!"}`,
	`{"id":  9, "msg":"Hello World"}`,
	`{"id": 10, "msg":"Hello World!"}`,
	`{"id": 11, "msg":"hello beautiful world"}`,
	`{"id": 12, "msg":"hello beautiful world!"}`,
	`{"id": 13, "msg":"Hello Beautiful World"}`,
	`{"id": 14, "msg":"Hello Beautiful World!"}`,
	`{"id": 15, "msg":"", "field":"hello"}`,
	`{"id": 16, "msg":"", "field":"Hello"}`,
	`{"id": 17, "msg":"", "field":"world"}`,
	`{"id": 18, "msg":"", "field":"World"}`,
	`{"id": 19, "msg":"", "field":"beautiful"}`,
	`{"id": 20, "msg":"", "field":"Beautiful"}`,
	`{"id": 21, "msg":"", "field":"hello world"}`,
	`{"id": 22, "msg":"", "field":"hello world!"}`,
	`{"id": 23, "msg":"", "field":"Hello World"}`,
	`{"id": 24, "msg":"", "field":"Hello World!"}`,
	`{"id": 25, "msg":"", "field":"hello beautiful world"}`,
	`{"id": 26, "msg":"", "field":"hello beautiful world!"}`,
	`{"id": 27, "msg":"", "field":"Hello Beautiful World"}`,
	`{"id": 28, "msg":"", "field":"Hello Beautiful World!"}`,
	`{"id": 30, "msg":"", "field":98}`,
	`{"id": 31, "msg":"", "field":"98"}`,
	`{"id": 32, "msg":"", "field":99}`,
	`{"id": 33, "msg":"", "field":"99"}`,
	`{"id": 34, "msg":"", "field":400}`,
	`{"id": 35, "msg":"", "field":"400"}`,
	`{"id": 36, "msg":"", "field":499}`,
	`{"id": 37, "msg":"", "field":"499"}`,
	`{"id": 38, "msg":"", "field":500}`,
	`{"id": 39, "msg":"", "field":"500"}`,
	`{"id": 40, "msg":"beautiful world"}`,
	`{"id": 41, "msg":"", "field":"beautiful world"}`,
	`{"id": 42, "msg":"world", "field":"hello"}`,
	`{"id": 43, "msg":"beautiful", "field":"hello world"}`,
	`{"id": 44, "msg":"beautiful", "field":"hello world", "count":99}`,
	`{"id": 45, "msg":"beautiful", "field":"hello world", "count":99, "status":200, "code":300}`,
	`{"id": 46, "msg":"beautiful", "field":"hello world", "count":99, "status":404, "code":300}`,
	`{"id": 47, "msg":"beautiful", "field":"hello world", "count":99, "status":404, "code":450}`,
	`{"id": 50, "msg":"hell\"o"}`,
	`{"id": 51, "msg":"hello \" world"}`,
	`{"id": 52, "msg":"hello \" world!"}`,
	`{"id": 53, "msg":"", "field":"hell\"o"}`,
	`{"id": 54, "msg":"", "field":"hello \" world"}`,
	`{"id": 55, "msg":"", "field":"hello \" world!"}`,
	`{"id": 56, "field":"my [beautiful] pet"}`,
	`{"id": 57, "field":"he[ll]\"o"}`,
	`{"id": 58, "field":"hell\"o"}`,
	`{"id": 59, "msg":"beautiful \" pet"}`,
	`{"id": 60, "msg":"pet]"}`,
	`{"id": 61, "msg":"hell\"o \"pet"}`,
	`{"id": 62, "path":"c:/dev/projects/sqlog"}`,
	`{"id": 63, "path":"c:/dev/projects/chain"}`,
	`{"id": 64, "msg":"hell\"o \"world"}`,
}

func Test_MemoryExprBasic(t *testing.T) {
	testCases := []testExprMemoryData{
		{
			"hello",
			[]int{1, 7, 8, 11, 12, 51, 52},
		},
		{
			"hello*",
			[]int{1, 7, 8, 11, 12, 51, 52},
		},
		{
			"hello world",
			[]int{1, 3, 7, 8, 11, 12, 40, 42, 51, 52, 64},
		},
		{
			"hello* *world",
			[]int{1, 3, 7, 8, 11, 12, 40, 42, 51, 52, 64},
		},
		{
			`"hello world"`,
			[]int{7},
		},
		{
			`"hello world*"`,
			[]int{7, 8},
		},
		{
			`"*hello*world*"`,
			[]int{7, 8, 11, 12, 51, 52},
		},
		{
			`field:hello`,
			[]int{15, 21, 22, 25, 26, 42, 43, 44, 45, 46, 47, 54, 55},
		},
		{
			"field:hello*",
			[]int{15, 21, 22, 25, 26, 42, 43, 44, 45, 46, 47, 54, 55},
		},
		{
			"field:hello world",
			[]int{3, 7, 8, 11, 12, 15, 21, 22, 25, 26, 40, 42, 43, 44, 45, 46, 47, 51, 52, 54, 55, 64},
		},
		{
			"field:hello* *world",
			[]int{3, 7, 11, 15, 21, 22, 25, 26, 40, 42, 43, 44, 45, 46, 47, 51, 54, 55, 64},
		},
		{
			`field:"hello world"`,
			[]int{21, 43, 44, 45, 46, 47},
		},
		{
			`field:"hello world*"`,
			[]int{21, 22, 43, 44, 45, 46, 47},
		},
		{
			`field:"*hello*world*"`,
			[]int{21, 22, 25, 26, 43, 44, 45, 46, 47, 54, 55},
		},
	}
	for _, tt := range testCases {
		runMemoryExprTest(t, tt)
	}
}

// Numerical values
func Test_MemoryExprNumerical(t *testing.T) {
	testCases := []testExprMemoryData{
		{
			`field:99`,
			[]int{32, 33},
		},
		{
			`field:>99`,
			[]int{34, 35, 36, 37, 38, 39},
		},
		{
			`field:<99`,
			[]int{30, 31},
		},
		{
			`field:>=99`,
			[]int{32, 33, 34, 35, 36, 37, 38, 39},
		},
		{
			`field:<=99`,
			[]int{30, 31, 32, 33},
		},
	}
	for _, tt := range testCases {
		runMemoryExprTest(t, tt)
	}
}

func Test_MemoryExprArray(t *testing.T) {
	testCases := []testExprMemoryData{
		{
			`[hello world]`,
			[]int{1, 3, 42},
		},
		{
			`[hello "beautiful world"]`,
			[]int{1, 40},
		},
		{
			`field:[hello world]`,
			[]int{15, 17, 42},
		},
		{
			`field:[hello "beautiful world"]`,
			[]int{15, 41, 42},
		},
		{
			`field:[400 TO 499]`,
			[]int{34, 35, 36, 37},
		},
		{
			`field:[99 400 500]`,
			[]int{32, 33, 34, 35, 38, 39},
		},
		{
			`field:[99 hello "beautiful world" 400 500]`,
			[]int{15, 32, 33, 34, 35, 38, 39, 41, 42},
		},
	}
	for _, tt := range testCases {
		runMemoryExprTest(t, tt)
	}
}

// Boolean Operators
func Test_MemoryExprBoolean(t *testing.T) {
	testCases := []testExprMemoryData{
		{
			"hello AND world",
			[]int{7, 8, 11, 12, 51, 52},
		},
		{
			"hello AND beautiful AND world",
			[]int{11, 12},
		},
		{
			"hello AND beautiful AND *world*",
			[]int{11, 12},
		},
		{
			"hello OR world",
			[]int{1, 3, 7, 8, 11, 12, 40, 42, 51, 52, 64},
		},
		{
			"field:hello AND world",
			[]int{42},
		},
		{
			"field:hello AND beautiful AND field:world",
			[]int{43, 44, 45, 46, 47},
		},
		{
			"field:hello OR world",
			[]int{3, 7, 8, 11, 12, 15, 21, 22, 25, 26, 40, 42, 43, 44, 45, 46, 47, 51, 52, 54, 55, 64},
		},
		{
			"hello AND (beautiful world)",
			[]int{7, 8, 11, 12, 51, 52},
		},
		{
			"hello AND (beautiful AND world)",
			[]int{11, 12},
		},
		{
			"field:hello AND (beautiful AND count:99)",
			[]int{44, 45, 46, 47},
		},
		{
			`(field:hello* OR world*) AND (field:[hello "beautiful world"] OR (count:99 AND status:[200 400 500]) OR code:[400 TO 499])`,
			[]int{15, 42, 45, 47},
		},
	}
	for _, tt := range testCases {
		runMemoryExprTest(t, tt)
	}
}

func Test_MemoryExprEscape(t *testing.T) {
	testCases := []testExprMemoryData{
		{
			`hell\"o`,
			[]int{50, 61, 64},
		},
		{
			`"hello \" world"`,
			[]int{51},
		},
		{
			`"hello \" world*"`,
			[]int{51, 52},
		},
		{
			`field:hell\"o`,
			[]int{53, 58},
		},
		{
			`field:"hello \" world"`,
			[]int{54},
		},
		{
			`field:"hello \" world*"`,
			[]int{54, 55},
		},
		{
			`field:"my [beautiful] pet*"`,
			[]int{56},
		},
		{
			`field:he\[ll]\"o`,
			[]int{57},
		},
		{
			`field:[hell\"o "beautiful \" pet"]`,
			[]int{53, 58},
		},
		{
			`field:[hell\"o pet\]]`,
			[]int{53, 58},
		},
		{
			`path:c\:/dev/projects/*`,
			[]int{62, 63},
		},
		{
			`(hell\"o AND \"pet)`,
			[]int{61},
		},
	}
	for _, tt := range testCases {
		runMemoryExprTest(t, tt)
	}
}

func Test_MemoryExprIncomplete(t *testing.T) {
	testCases := []testExprMemoryData{
		{
			`"hello \" world`,
			[]int{51},
		},
		{
			`field:[hell\"o "beautiful \" world"`,
			[]int{53, 58},
		},
		{
			`field:[hell\"o world\]`,
			[]int{53, 58},
		},
		{
			`field:[]`,
			[]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64},
		},
		{
			`field:[     ]`,
			[]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64},
		},
		{
			`(hell\"o AND \"world`,
			[]int{64},
		},
		{
			`(field:hello* OR world*) AND (field:[hello "beautiful world"] OR (field:99 AND field:[100 200 300`,
			[]int{15, 42},
		},
	}
	for _, tt := range testCases {
		runMemoryExprTest(t, tt)
	}
}

func runMemoryExprTest(t *testing.T, tt testExprMemoryData) {
	expr, err := MemoryExprBuilderFn(tt.expr)
	assert.NoError(t, err)

	var ids []int

	for _, e := range testExpMemoryEntries {
		if expr(e) {
			var c map[string]interface{}
			err := json.Unmarshal(e.Content, &c)
			if err == nil {
				ids = append(ids, int(c["id"].(float64)))
			}

		}
	}

	assert.Equal(t, tt.ids, ids, "exp=%s", tt.expr)
}
