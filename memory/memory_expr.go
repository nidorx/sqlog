package memory

import (
	"encoding/json"
	"strconv"
	"unsafe"

	"github.com/nidorx/sqlog"
)

var (
	MemoryExprBuilderFn = sqlog.NewExprBuilder(func(expression string) (sqlog.ExprBuilder[MemoryExpr], string) {
		return &MemoryExprBuilder{}, expression
	})
)

// MemoryExpr is the structure that represents the expression to be evaluated in memory.
type MemoryExpr func(e *sqlog.Entry) bool

// MemoryExprBuilder is used to build memory expressions.
type MemoryExprBuilder struct {
	stack      []memExpr
	groupStack [][]memExpr // Stack to handle grouping of expressions
}

// Build returns the final composed expression to be applied to the Entry.
func (m *MemoryExprBuilder) Build() MemoryExpr {
	stack := m.stack
	if len(stack) == 0 {
		return func(e *sqlog.Entry) bool {
			return true
		}
	}
	return func(e *sqlog.Entry) bool {
		var j map[string]any
		if err := json.Unmarshal(e.Content, &j); err != nil {
			return false
		}
		for _, expr := range stack {
			if !expr.eval(e, j) {
				return false
			}
		}
		return true
	}
}

func (m *MemoryExprBuilder) add(expr memExpr) {
	if len(m.stack) > 0 {
		last := m.stack[len(m.stack)-1]
		if exprAnd, ok := last.(*memExprAND); ok {
			exprAnd.stack = append(exprAnd.stack, expr)
			return
		}

		if exprOr, ok := last.(*memExprOR); ok {
			exprOr.stack = append(exprOr.stack, expr)
			return
		}
	}

	m.stack = append(m.stack, expr)
}

// Operator appends logical operators (AND, OR) between expressions.
func (m *MemoryExprBuilder) Operator(op string) {
	if len(m.stack) == 0 {
		return
	}

	last := m.stack[len(m.stack)-1]

	if op == "AND" {
		if _, ok := last.(*memExprAND); !ok {
			m.stack[len(m.stack)-1] = &memExprAND{
				stack: []memExpr{last},
			}
		}
	} else {
		if _, ok := last.(*memExprOR); !ok {
			m.stack[len(m.stack)-1] = &memExprOR{
				stack: []memExpr{last},
			}
		}
	}
}

// GroupStart creates a new group of expressions.
func (m *MemoryExprBuilder) GroupStart() {
	// Push the current stack to the group stack, and start a new group
	m.groupStack = append(m.groupStack, m.stack)
	m.stack = nil // Start a fresh group
}

// GroupEnd finalizes the grouping of expressions and merges with the parent stack.
func (m *MemoryExprBuilder) GroupEnd() {
	if len(m.groupStack) == 0 {
		return // No group to end
	}

	// Get the last group (current group)
	groupExpr := &memExprGroup{stack: m.stack}

	// Restore the previous stack
	lastGroupIndex := len(m.groupStack) - 1
	m.stack = m.groupStack[lastGroupIndex]       // Restore parent group
	m.groupStack = m.groupStack[:lastGroupIndex] // Remove the current group from stack

	// Add the group expression to the parent stack
	m.add(groupExpr)
}

// Text checks if a text field in the log matches the specified term.
func (m *MemoryExprBuilder) Text(field, term string, isSequence, isWildcard bool) {
	m.add(&memExprText{
		field:      field,
		term:       term,
		isSequence: isSequence,
		isWildcard: isWildcard,
	})
}

// Number checks if a numeric field matches the condition with the specified value.
func (m *MemoryExprBuilder) Number(field, condition string, value float64) {
	m.add(&memExprNumber{
		field:     field,
		condition: condition,
		value:     value,
	})
}

// Between checks if a numeric field is between two values.
func (m *MemoryExprBuilder) Between(field string, x, y float64) {
	m.add(&memExprBetween{
		field: field,
		x:     x,
		y:     y,
	})
}

// TextIn checks if a text field matches one of the values in a list.
func (m *MemoryExprBuilder) TextIn(field string, values []string) {
	m.add(&memExprTextIn{
		field:  field,
		values: values,
	})
}

// NumberIn checks if a numeric field matches one of the values in a list.
func (m *MemoryExprBuilder) NumberIn(field string, values []float64) {
	m.add(&memExprNumberIn{
		field:  field,
		values: values,
	})
}

type memExpr interface {
	eval(e *sqlog.Entry, j map[string]any) bool
}

type memExprGroup struct {
	stack []memExpr
}

func (m *memExprGroup) eval(e *sqlog.Entry, j map[string]any) bool {
	for _, expr := range m.stack {
		if !expr.eval(e, j) {
			return false
		}
	}
	return true
}

type memExprOR struct {
	stack []memExpr
}

func (m *memExprOR) eval(e *sqlog.Entry, j map[string]any) bool {
	for _, expr := range m.stack {
		if expr.eval(e, j) {
			return true
		}
	}
	return false
}

type memExprAND struct {
	stack []memExpr
}

func (m *memExprAND) eval(e *sqlog.Entry, j map[string]any) bool {
	for _, expr := range m.stack {
		if !expr.eval(e, j) {
			return false
		}
	}
	return true
}

type memExprText struct {
	field      string
	term       string
	isSequence bool
	isWildcard bool
}

func (m *memExprText) eval(e *sqlog.Entry, j map[string]any) bool {
	var (
		fieldValue string
		field      = m.field
		term       = m.term
		isSequence = m.isSequence
		isWildcard = m.isWildcard
	)

	fieldValue, valid := memExprGetText(j, field)
	if !valid {
		return false
	}

	if isSequence {
		if isWildcard {
			return wildcardMatch(term, fieldValue)
		}
		return fieldValue == term
	}

	if isWildcard {
		return wildcardMatch(term, fieldValue)
	}
	return wildcardMatch("*"+term+"*", fieldValue)
}

type memExprNumber struct {
	field     string
	condition string
	value     float64
}

func (m *memExprNumber) eval(e *sqlog.Entry, j map[string]any) bool {
	var (
		fieldValue float64
		field      = m.field
		condition  = m.condition
		value      = m.value
	)

	fieldValue, valid := memExprGetNumber(j, field)
	if !valid {
		return false
	}

	switch condition {
	case "=":
		return fieldValue == value
	case ">":
		return fieldValue > value
	case "<":
		return fieldValue < value
	case ">=":
		return fieldValue >= value
	case "<=":
		return fieldValue <= value
	}

	return false
}

type memExprBetween struct {
	field string
	x, y  float64
}

func (m *memExprBetween) eval(e *sqlog.Entry, j map[string]any) bool {
	var (
		fieldValue float64
		field      = m.field
		x          = m.x
		y          = m.y
	)

	fieldValue, valid := memExprGetNumber(j, field)
	if !valid {
		return false
	}

	return fieldValue >= x && fieldValue <= y
}

type memExprTextIn struct {
	field  string
	values []string
}

func (m *memExprTextIn) eval(e *sqlog.Entry, j map[string]any) bool {
	var (
		fieldValue string
		field      = m.field
		values     = m.values
	)

	fieldValue, valid := memExprGetText(j, field)
	if !valid {
		return false
	}

	for _, v := range values {
		if fieldValue == v {
			return true
		}
	}
	return false
}

type memExprNumberIn struct {
	field  string
	values []float64
}

func (m *memExprNumberIn) eval(e *sqlog.Entry, j map[string]any) bool {
	var (
		fieldValue float64
		field      = m.field
		values     = m.values
	)

	fieldValue, valid := memExprGetNumber(j, field)
	if !valid {
		return false
	}

	for _, v := range values {
		if fieldValue == v {
			return true
		}
	}
	return false
}

func memExprGetNumber(j map[string]any, field string) (fieldValue float64, valid bool) {
	if v, ok := j[field]; !ok {
		return 0, false
	} else {
		switch tv := v.(type) {
		case float64:
			fieldValue = tv
		case string:
			if n, err := strconv.ParseFloat(tv, 64); err != nil {
				return 0, false
			} else {
				fieldValue = n
			}
		default:
			return 0, false
		}
	}

	return fieldValue, true
}

func memExprGetText(j map[string]any, field string) (fieldValue string, valid bool) {
	if v, ok := j[field]; !ok {
		return "", false
	} else {
		switch tv := v.(type) {
		case string:
			fieldValue = tv
		default:
			// cache json.Marshal during this execution
			if s, ok := j["___cs"+field]; !ok {
				if b, err := json.Marshal(tv); err != nil {
					j["___cs"+field] = ""
					return "", false
				} else {
					fieldValue = unsafe.String(unsafe.SliceData(b), len(b))
					j["___cs"+field] = fieldValue
				}
				return "", false
			} else {
				fieldValue = s.(string)
			}
		}
	}
	if fieldValue == "" {
		return "", false
	}

	return fieldValue, true
}
