package sqlog

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

var compiledCache = sync.Map{}

type compiledExpr struct {
	Sql  string
	Args []any
}

// compileExpr a expression into an unbound query (using the '?' bindvar).
func compileExpr(expression string) (*compiledExpr, error) {
	expression = strings.TrimSpace(expression)

	if c, ok := compiledCache.Load(expression); ok {
		return c.(*compiledExpr), nil
	}

	s := &compileExprState{
		args:  []any{},
		sql:   bytes.NewBuffer(make([]byte, 0, 512)),
		buf:   bytes.NewBuffer(make([]byte, 0, 256)),
		field: bytes.NewBuffer(make([]byte, 0, 10)),
	}

	var (
		i    int
		b    byte
		qs   = []byte(expression)
		last = len(qs) - 1
	)

	for ; i <= last; i++ {
		b = qs[i]

		if b == '(' && !s.inArray && !s.inQuote {
			// faz a compilacao de todos os grupos internos
			var (
				inner        = bytes.NewBuffer(make([]byte, 0, 512))
				parenthesis  = 1 // inner parenthesis
				innerInQuote = false
				j            = i + 1 // ignore (
			)
			for ; j <= last; j++ {
				c := qs[j]

				if c == '(' && !innerInQuote {
					parenthesis++
					inner.WriteByte(c)
				} else if c == ')' && !innerInQuote {
					parenthesis--
					if parenthesis == 0 {
						break
					} else {
						inner.WriteByte(c)
					}
				} else if c == '"' {
					// is escaped (Ex. `error:myMethod\(\"trace\"\)`)? append a '"'
					if qs[j-1] != '\\' {
						if innerInQuote {
							innerInQuote = false
						} else {
							innerInQuote = true
						}
					}
					inner.WriteByte(c)
				} else {
					inner.WriteByte(c)
				}
			}

			substr := inner.String()
			subCompiled, subErr := compileExpr(substr)
			if subErr != nil {
				return nil, errors.Join(fmt.Errorf("invalid expression %s ", substr), subErr)
			}

			s.appendOperator()

			s.sql.WriteByte('(')
			s.sql.WriteString(subCompiled.Sql)
			s.sql.WriteByte(')')
			s.args = append(s.args, subCompiled.Args...)

			i = j
		} else if b == '[' && !s.inQuote {
			if i > 0 && qs[i-1] == '\\' {
				s.buf.Truncate(s.buf.Len() - 1)
				s.buf.WriteByte('[')
			} else if s.inArray {
				// a '[' while we're in a array is an error
				return nil, errors.New("unexpected `[` at " + strconv.Itoa(i))
			} else {
				s.inArray = true
			}
		} else if b == ']' && s.inArray && !s.inQuote {
			if i > 0 && qs[i-1] == '\\' {
				s.buf.Truncate(s.buf.Len() - 1)
				s.buf.WriteByte(']')
			} else {
				s.appendSingleTerm()
				if err := s.closeArray(); err != nil {
					return nil, err
				}
			}
		} else if b == ' ' {
			if s.inQuote {
				s.buf.WriteByte(b)
			} else {
				s.appendSingleTerm()
			}
		} else if b == '"' {
			// is escaped (Ex. `error:myMethod\(\"trace\"\)`)? append a '"'
			if i > 0 && qs[i-1] == '\\' {
				s.buf.Truncate(s.buf.Len() - 1)
				s.buf.WriteByte('"')
			} else if s.inQuote {
				s.inQuote = false
				s.appendSequence()
			} else {
				s.inQuote = true
			}
		} else if b == ':' && !s.inQuote {
			// is escaped (Ex. "path:c\:/my/path")? append a ':'
			if i > 0 && qs[i-1] == '\\' {
				s.buf.Truncate(s.buf.Len() - 1)
				s.buf.WriteByte(':')
			} else if s.field.Len() > 0 {
				// a ':' while we're in a name is an error
				return nil, errors.New("unexpected `:` at " + strconv.Itoa(i))
			} else {
				if f := strings.TrimSpace(s.buf.String()); f != "" {
					s.field.WriteString(f)
				}
				s.buf.Reset()
			}
		} else {
			// salva no buffer
			s.buf.WriteByte(b)
		}
	}

	// add last part
	if s.inQuote {
		s.appendSequence()
	} else {
		s.appendSingleTerm()
	}
	if err := s.closeArray(); err != nil {
		return nil, err
	}

	compiled := &compiledExpr{
		Sql:  s.sql.String(),
		Args: s.args,
	}

	compiledCache.Store(expression, compiled)

	return compiled, nil
}

type compileExprState struct {
	args       []any
	inQuote    bool
	inArray    bool
	operator   string
	arrayParts []string
	sql        *bytes.Buffer // final query
	buf        *bytes.Buffer // current value
	field      *bytes.Buffer // current field name
}

func (s *compileExprState) appendOperator() {
	if s.sql.Len() > 0 {
		if s.operator == "" {
			s.sql.WriteString(" OR ")
		} else {
			s.sql.WriteString(s.operator)
		}
	}
	s.operator = ""
}

func (s *compileExprState) closeArray() error {
	if !s.inArray {
		return nil
	}
	s.inArray = false

	if len(s.arrayParts) == 0 {
		return nil
	}

	s.appendOperator()

	fieldName := "msg"
	if s.field.Len() > 0 {
		fieldName = s.field.String()
	}

	if len(s.arrayParts) == 3 && s.arrayParts[1] == "TO" {
		// field:[400 TO 499]

		s.sql.WriteString("CAST(json_extract(e.content, ?) AS NUMERIC) BETWEEN ? AND ?")
		s.args = append(s.args, "$."+fieldName)

		if n, err := strconv.ParseFloat(s.arrayParts[0], 64); err != nil {
			return errors.New("invalid clause [" + strings.Join(s.arrayParts, "") + "]")
		} else {
			s.args = append(s.args, n)
		}

		if n, err := strconv.ParseFloat(s.arrayParts[2], 64); err != nil {
			return errors.New("invalid clause [" + strings.Join(s.arrayParts, "") + "]")
		} else {
			s.args = append(s.args, n)
		}
	} else {

		var (
			textArgs   []any
			numberArgs []any
		)

		for _, v := range s.arrayParts {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				numberArgs = append(numberArgs, n)
			} else {
				textArgs = append(textArgs, v)
			}
		}

		group := len(numberArgs) > 0 && len(textArgs) > 0

		if group {
			s.sql.WriteByte('(')
		}

		if len(numberArgs) > 0 {
			s.args = append(s.args, "$."+fieldName)
			s.sql.WriteString("CAST(json_extract(e.content, ?) AS NUMERIC) IN (")
			for i, v := range numberArgs {
				if i > 0 {
					s.sql.WriteByte(',')
				}
				s.sql.WriteByte('?')
				s.args = append(s.args, v)
			}
			s.sql.WriteByte(')')
		}

		if group {
			s.sql.WriteString(" OR ")
		}

		if len(textArgs) > 0 {
			s.args = append(s.args, "$."+fieldName)
			s.sql.WriteString("json_extract(e.content, ?) IN (")
			for i, v := range textArgs {
				if i > 0 {
					s.sql.WriteByte(',')
				}
				s.sql.WriteByte('?')
				s.args = append(s.args, v)
			}
			s.sql.WriteByte(')')
		}

		if group {
			s.sql.WriteByte(')')
		}
	}

	s.arrayParts = nil

	return nil
}

// appendSingleTerm a single term is a single word such as test or hello.
func (s *compileExprState) appendSingleTerm() {

	if s.inArray {
		if s.buf.Len() > 0 {
			s.arrayParts = append(s.arrayParts, s.buf.String())
		}
		s.buf.Reset()
		return
	}

	if s.buf.Len() > 0 {

		text := s.buf.String()
		textUp := strings.ToUpper(text)
		if textUp == "AND" || textUp == "OR" {
			s.operator = " " + textUp + " "
			s.buf.Reset()
			return
		}

		s.appendOperator()

		var (
			number          float64
			isNumeric       bool
			numberCondition string
		)

		fieldName := "msg"
		if s.field.Len() > 0 {
			fieldName = s.field.String()

			if strings.HasPrefix(text, ">") || strings.HasPrefix(text, "<") {
				// Numerical values ?
				var numberStr string
				for _, cond := range []string{">=", ">", "<=", "<"} {
					if strings.HasPrefix(text, cond) {
						numberCondition = cond
						numberStr = strings.TrimPrefix(text, cond)
						break
					}
				}

				if n, err := strconv.ParseFloat(numberStr, 64); err == nil {
					number = n
					isNumeric = true
				}
			} else if n, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
				number = n
				isNumeric = true
				numberCondition = "="
			}
		}

		if isNumeric {
			s.sql.WriteString("CAST(json_extract(e.content, ?) AS NUMERIC) ")
			s.sql.WriteString(numberCondition)
			s.sql.WriteString(" ? ")

			s.args = append(s.args, "$."+fieldName, number)
			s.buf.Reset()
			s.field.Reset()
		} else {
			s.sql.WriteString("json_extract(e.content, ?) LIKE ?")

			s.args = append(s.args, "$."+fieldName)

			if strings.LastIndexByte(text, '*') >= 0 {
				s.args = append(s.args, strings.ReplaceAll(text, "*", "%"))
			} else {
				s.args = append(s.args, "%"+text+"%")
			}
		}
	}
	s.buf.Reset()
	s.field.Reset()
}

// appendSequence a sequence is a group of words surrounded by double quotes, such as "hello world".
func (s *compileExprState) appendSequence() {
	if s.inArray {
		s.arrayParts = append(s.arrayParts, s.buf.String())
		s.buf.Reset()
		return
	}

	if s.buf.Len() > 0 {
		s.appendOperator()

		fieldName := "msg"
		if s.field.Len() > 0 {
			fieldName = s.field.String()
		}
		s.args = append(s.args, "$."+fieldName)

		text := s.buf.String()
		if strings.LastIndexByte(text, '*') >= 0 {
			s.sql.WriteString("json_extract(e.content, ?) LIKE ?")
			s.args = append(s.args, strings.ReplaceAll(text, "*", "%"))
		} else {
			s.sql.WriteString("json_extract(e.content, ?) = ?")
			s.args = append(s.args, text)
		}
	}
	s.buf.Reset()
	s.field.Reset()
}
