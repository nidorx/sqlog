package litelog

import (
	"bytes"
	"strings"
	"time"
)

var (
	sqlSeekPageAfter       = []byte("SELECT e.epoch_secs, e.nanos, e.level, e.content FROM entries e WHERE (e.epoch_secs > ? OR (e.epoch_secs = ? AND e.nanos > ?)) ")
	sqlSeekPageBefore      = []byte("SELECT e.epoch_secs, e.nanos, e.level, e.content FROM entries e WHERE (e.epoch_secs < ? OR (e.epoch_secs = ? AND e.nanos < ?)) ")
	sqlSeekPageAfterOrder  = []byte(" ORDER BY e.epoch_secs ASC, e.nanos ASC LIMIT ?")
	sqlSeekPageBeforeOrder = []byte(" ORDER BY e.epoch_secs DESC, e.nanos DESC LIMIT ?")
)

// seekEntries obtém uma página de resultados (seek method or keyset pagination).
// A ordenação é inversa, o resultado mais antigo vem primeiro
func (s *store) seekEntries(expr string, levels map[string]bool, direction string, epochEnd int64, nanosEnd, limitResults int) ([]any, error) {

	if epochEnd == 0 {
		epochEnd = time.Now().UTC().Unix()
	}

	buf := bytes.NewBuffer(make([]byte, 0, 128))

	if direction == "before" {
		buf.Write(sqlSeekPageBefore)
	} else {
		buf.Write(sqlSeekPageAfter)
	}
	args := []any{epochEnd, epochEnd, nanosEnd}

	if len(levels) > 0 && len(levels) != 4 {
		buf.WriteString(" AND (")

		if levels["error"] && levels["warn"] && levels["info"] {
			buf.WriteString(" e.level >= 0 ")
		} else {
			clause := ""

			if levels["error"] && levels["warn"] {
				buf.WriteString(" e.level >= 4 ")
				clause = " OR "
			} else {
				if levels["error"] {
					buf.WriteString(" e.level >= 8 ")
					clause = " OR "
				} else if levels["warn"] {
					buf.WriteString(" (e.level BETWEEN 4 AND 7) ")
					clause = " OR "
				}
			}

			if levels["info"] {
				buf.WriteString(clause)
				buf.WriteString(" (e.level BETWEEN 0 AND 3) ")
				clause = " OR "
			}

			if levels["debug"] {
				buf.WriteString(clause)
				buf.WriteString(" e.level < 0 ")
			}
		}
		buf.WriteString(") ")
	}

	if expr = strings.TrimSpace(expr); expr != "" {
		if compiled, err := Compile(expr); err != nil {
			return nil, err
		} else if compiled.Sql != "" {
			buf.WriteString(" AND (")
			buf.WriteString(compiled.Sql)
			buf.WriteByte(')')
			args = append(args, compiled.Args...)
		}
	}

	if direction == "before" {
		buf.Write(sqlSeekPageBeforeOrder)
	} else {
		buf.Write(sqlSeekPageAfterOrder)
	}
	args = append(args, min(max(limitResults, 10), 100))

	stm, err := s.db.Prepare(buf.String())
	if err != nil {
		return nil, err
	}
	defer stm.Close()

	rows, err := stm.Query(args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []any

	for rows.Next() {
		var (
			epoch   int64
			nanos   int
			level   int
			content string
		)

		if err = rows.Scan(&epoch, &nanos, &level, &content); err != nil {
			return nil, err
		}

		list = append(list, []any{epoch, nanos, level, content})
	}

	return list, nil
}
