package sqlog

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// ExprBuilder expression builder interface
type ExprBuilder[E any] interface {
	Build() E
	GroupStart()
	GroupEnd()
	Operator(op string)
	Text(field, term string, isSequence, isWildcard bool)
	Number(field, condition string, value float64)
	Between(field string, x, y float64)
	TextIn(field string, values []string)
	NumberIn(field string, values []float64)
}

type ExprBuilderFactory[E any] func(expression string) (ExprBuilder[E], string)

// NewExprBuilder creates a new expression builder.
// Allows the use of the same filter pattern in different dialects (Ex. Memory, Sqlite, PostgreSQL)
func NewExprBuilder[E any](factory ExprBuilderFactory[E]) func(expression string) (E, error) {

	var parse func(expression string, builder ExprBuilder[E]) error

	parse = func(expression string, builder ExprBuilder[E]) error {
		var (
			i    int
			b    byte
			qs   = []byte(expression)
			last = len(qs) - 1
			s    = &exprParseState[E]{
				buf:     bytes.NewBuffer(make([]byte, 0, 256)),
				field:   bytes.NewBuffer(make([]byte, 0, 10)),
				builder: builder,
			}
		)

		for ; i <= last; i++ {
			b = qs[i]

			if b == '(' && !s.inArray && !s.inQuote {
				// compiles all internal groups
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

				s.addOperator()

				s.builder.GroupStart()

				substr := inner.String()
				subErr := parse(substr, builder)
				if subErr != nil {
					return errors.Join(fmt.Errorf("invalid expression %s ", substr), subErr)
				}

				s.builder.GroupEnd()
				s.dirty = true

				i = j
			} else if b == '[' && !s.inQuote {
				if i > 0 && qs[i-1] == '\\' {
					s.buf.Truncate(s.buf.Len() - 1)
					s.buf.WriteByte('[')
				} else if s.inArray {
					// a '[' while we're in a array is an error
					return errors.New("unexpected `[` at " + strconv.Itoa(i))
				} else {
					s.inArray = true
				}
			} else if b == ']' && s.inArray && !s.inQuote {
				if i > 0 && qs[i-1] == '\\' {
					s.buf.Truncate(s.buf.Len() - 1)
					s.buf.WriteByte(']')
				} else {
					s.addTermSingle()
					if err := s.closeArray(); err != nil {
						return err
					}
				}
			} else if b == ' ' {
				if s.inQuote {
					s.buf.WriteByte(b)
				} else {
					s.addTermSingle()
				}
			} else if b == '"' {
				// is escaped (Ex. `error:myMethod\(\"trace\"\)`)? append a '"'
				if i > 0 && qs[i-1] == '\\' {
					s.buf.Truncate(s.buf.Len() - 1)
					s.buf.WriteByte('"')
				} else if s.inQuote {
					s.inQuote = false
					s.addTermSequence()
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
					return errors.New("unexpected `:` at " + strconv.Itoa(i))
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
			s.addTermSequence()
		} else {
			s.addTermSingle()
		}

		if err := s.closeArray(); err != nil {
			return err
		}

		return nil
	}

	var cache = sync.Map{}

	// the builder
	return func(expression string) (exp E, err error) {
		expression = strings.TrimSpace(expression)

		if c, ok := cache.Load(expression); ok {
			return c.(E), nil
		}

		mapper, newExpression := factory(expression)

		err = parse(newExpression, mapper)
		if err != nil {
			return
		}

		exp = mapper.Build()
		cache.Store(expression, exp)

		return exp, nil
	}
}
