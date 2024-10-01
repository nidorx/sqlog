package sqlite

import (
	"bytes"
	"sqlog"
)

var (
	ExpBuilderFn = sqlog.NewExprBuilder(func(expression string) (sqlog.ExprBuilder[*Expr], string) {
		return &SqliteExprBuilder{
			args: []any{},
			sql:  bytes.NewBuffer(make([]byte, 0, 512)),
		}, expression
	})
)

type Expr struct {
	Sql  string
	Args []any
}

type SqliteExprBuilder struct {
	args   []any
	sql    *bytes.Buffer
	groups []*bytes.Buffer
}

func (s *SqliteExprBuilder) Build() *Expr {
	// @TODO: write all opened s.groups
	return &Expr{
		Sql:  s.sql.String(),
		Args: s.args,
	}
}

func (s *SqliteExprBuilder) GroupStart() {
	s.sql.WriteByte('(')
	s.groups = append(s.groups, s.sql)
	s.sql = bytes.NewBuffer(make([]byte, 0, 512))
}

func (s *SqliteExprBuilder) GroupEnd() {
	if len(s.groups) > 0 {
		// sempre espera que sim
		last := len(s.groups) - 1
		parent := s.groups[last]
		s.groups = s.groups[:last]
		parent.Write(s.sql.Bytes())
		s.sql = parent
	}
	s.sql.WriteByte(')')
}

func (s *SqliteExprBuilder) Operator(op string) {
	if s.sql.Len() > 0 {
		s.sql.WriteByte(' ')
		s.sql.WriteString(op) // AND|OR
		s.sql.WriteByte(' ')
	}
}

func (s *SqliteExprBuilder) Text(field, term string, isSequence, isWildcard bool) {
	field = "$." + field
	if isSequence {
		if isWildcard {
			s.sql.WriteString("json_extract(e.content, ?) GLOB ?")
			s.args = append(s.args, field, term)
		} else {
			s.sql.WriteString("json_extract(e.content, ?) = ?")
			s.args = append(s.args, field, term)
		}
	} else {
		s.sql.WriteString("json_extract(e.content, ?) GLOB ?")
		if isWildcard {
			s.args = append(s.args, field, term)
		} else {
			s.args = append(s.args, field, "*"+term+"*")
		}
	}
}

func (s *SqliteExprBuilder) TextIn(field string, values []string) {
	s.args = append(s.args, "$."+field)
	s.sql.WriteString("json_extract(e.content, ?) IN (")
	for i, v := range values {
		if i > 0 {
			s.sql.WriteByte(',')
		}
		s.sql.WriteByte('?')
		s.args = append(s.args, v)
	}
	s.sql.WriteByte(')')
}

func (s *SqliteExprBuilder) Number(field, condition string, value float64) {
	s.sql.WriteString("CAST(json_extract(e.content, ?) AS NUMERIC) ")
	s.sql.WriteString(condition)
	s.sql.WriteString(" ? ")
	s.args = append(s.args, "$."+field, value)
}

func (s *SqliteExprBuilder) Between(field string, x, y float64) {
	s.sql.WriteString("CAST(json_extract(e.content, ?) AS NUMERIC) BETWEEN ? AND ?")
	s.args = append(s.args, "$."+field, x, y)
}

func (s *SqliteExprBuilder) NumberIn(field string, values []float64) {
	s.sql.WriteString("CAST(json_extract(e.content, ?) AS NUMERIC) IN (")
	s.args = append(s.args, "$."+field)
	for i, v := range values {
		if i > 0 {
			s.sql.WriteByte(',')
		}
		s.sql.WriteByte('?')
		s.args = append(s.args, v)
	}
	s.sql.WriteByte(')')
}
