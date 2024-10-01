package sqlog

import (
	"bytes"
	"strings"
)

type SqliteExprMapper struct {
	args   []any
	sql    *bytes.Buffer // final query
	groups []*bytes.Buffer
}

func (s *SqliteExprMapper) Sql() string {
	return s.sql.String()
}

func (s *SqliteExprMapper) Args() []any {
	return s.args
}

func (s *SqliteExprMapper) GroupEnd() {
	if len(s.groups) > 0 { // sempre espera que sim
		last := len(s.groups) - 1
		parent := s.groups[last]
		s.groups = s.groups[:last]
		parent.Write(s.sql.Bytes())
		s.sql = parent
	}
	s.sql.WriteByte(')')
}

func (s *SqliteExprMapper) GroupStart() {
	s.sql.WriteByte('(')
	s.groups = append(s.groups, s.sql)
	s.sql = bytes.NewBuffer(make([]byte, 0, 512))
}

func (s *SqliteExprMapper) Operator(op string) {
	if s.sql.Len() > 0 {
		s.sql.WriteByte(' ')
		s.sql.WriteString(op) // AND|OR
		s.sql.WriteByte(' ')
	}
}

func (s *SqliteExprMapper) Term(field, term string, sequence, regex bool) {
	field = "$." + field
	if sequence {
		if regex {
			s.sql.WriteString("json_extract(e.content, ?) LIKE ?")
			s.args = append(s.args, field, strings.ReplaceAll(term, "*", "%"))
		} else {
			s.sql.WriteString("json_extract(e.content, ?) = ?")
			s.args = append(s.args, field, term)
		}
	} else {
		s.sql.WriteString("json_extract(e.content, ?) LIKE ?")
		if regex {
			s.args = append(s.args, field, strings.ReplaceAll(term, "*", "%"))
		} else {
			s.args = append(s.args, field, "%"+term+"%")
		}
	}
}

func (s *SqliteExprMapper) In(field string, values []string) {
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

func (s *SqliteExprMapper) Number(field, condition string, value float64) {
	s.sql.WriteString("CAST(json_extract(e.content, ?) AS NUMERIC) ")
	s.sql.WriteString(condition)
	s.sql.WriteString(" ? ")
	s.args = append(s.args, "$."+field, value)
}

func (s *SqliteExprMapper) NumberBetween(field string, x, y float64) {
	s.sql.WriteString("CAST(json_extract(e.content, ?) AS NUMERIC) BETWEEN ? AND ?")
	s.args = append(s.args, "$."+field, x, y)
}

func (s *SqliteExprMapper) NumberIn(field string, values []float64) {
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
