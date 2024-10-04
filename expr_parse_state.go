package sqlog

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
)

type exprParseState[E any] struct {
	builder    ExprBuilder[E]
	inQuote    bool
	inArray    bool
	operator   string
	arrayParts []string
	dirty      bool
	buf        *bytes.Buffer // current value
	field      *bytes.Buffer // current field name
}

func (s *exprParseState[E]) addOperator() {
	if s.dirty {
		if s.operator == "" {
			s.builder.Operator("AND")
		} else {
			s.builder.Operator(s.operator)
		}
	}

	s.operator = ""
}

func (s *exprParseState[E]) closeArray() error {
	if !s.inArray {
		return nil
	}
	s.inArray = false

	if len(s.arrayParts) == 0 {
		return nil
	}

	s.addOperator()

	fieldName := "msg"
	if s.field.Len() > 0 {
		fieldName = s.field.String()
	}

	if len(s.arrayParts) == 3 && s.arrayParts[1] == "TO" {
		// field:[400 TO 499]

		x, err := strconv.ParseFloat(s.arrayParts[0], 64)
		if err != nil {
			return errors.New("invalid clause [" + strings.Join(s.arrayParts, "") + "]")
		}

		y, err := strconv.ParseFloat(s.arrayParts[2], 64)
		if err != nil {
			return errors.New("invalid clause [" + strings.Join(s.arrayParts, "") + "]")
		}

		s.builder.Between(fieldName, x, y)

	} else {

		var (
			textArgs   []string
			numberArgs []float64
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
			s.builder.GroupStart()
		}

		if len(numberArgs) > 0 {
			if len(numberArgs) == 1 {
				s.builder.Number(fieldName, "=", numberArgs[0])
			} else {
				s.builder.NumberIn(fieldName, numberArgs)
			}
		}

		if group {
			s.builder.Operator("OR")
		}

		if len(textArgs) > 0 {
			s.builder.TextIn(fieldName, textArgs)
		}

		if group {
			s.builder.GroupEnd()
		}
	}

	s.dirty = true
	s.arrayParts = nil

	return nil
}

// addTermSingle a single term is a single word such as test or hello.
func (s *exprParseState[E]) addTermSingle() {

	if s.inArray {
		if s.buf.Len() > 0 {
			s.arrayParts = append(s.arrayParts, s.buf.String())
		}
		s.buf.Reset()
		return
	}

	if s.buf.Len() > 0 {

		var (
			number          float64
			isNumeric       bool
			numberCondition string
			text            = s.buf.String()
			textUpper       = strings.ToUpper(text)
		)
		if textUpper == "AND" || textUpper == "OR" {
			s.operator = textUpper
			s.buf.Reset()
			return
		}

		s.addOperator()

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
			s.builder.Number(fieldName, numberCondition, number)
		} else {
			s.builder.Text(fieldName, text, false, strings.LastIndexByte(text, '*') >= 0 || strings.LastIndexByte(text, '?') >= 0)
		}

		s.dirty = true
	}
	s.buf.Reset()
	s.field.Reset()
}

// addTermSequence a sequence is a group of words surrounded by double quotes, such as "hello world".
func (s *exprParseState[E]) addTermSequence() {
	if s.inArray {
		s.arrayParts = append(s.arrayParts, s.buf.String())
		s.buf.Reset()
		return
	}

	if s.buf.Len() > 0 {
		s.addOperator()

		fieldName := "msg"
		if s.field.Len() > 0 {
			fieldName = s.field.String()
		}

		text := s.buf.String()

		s.builder.Text(fieldName, text, true, strings.LastIndexByte(text, '*') >= 0 || strings.LastIndexByte(text, '?') >= 0)
		s.dirty = true
	}
	s.buf.Reset()
	s.field.Reset()
}
