package sqlite

import (
	"bytes"
	"sort"
	"strings"
	"time"

	"github.com/nidorx/sqlog"
)

var (
	sqlSeekPageAfter       = []byte("SELECT e.epoch_secs, e.nanos, e.level, e.content FROM entries e WHERE (e.epoch_secs > ? OR (e.epoch_secs = ? AND e.nanos > ?)) ")
	sqlSeekPageBefore      = []byte("SELECT e.epoch_secs, e.nanos, e.level, e.content FROM entries e WHERE (e.epoch_secs < ? OR (e.epoch_secs = ? AND e.nanos < ?)) ")
	sqlSeekPageAfterOrder  = []byte(" ORDER BY e.epoch_secs ASC, e.nanos ASC LIMIT ?")
	sqlSeekPageBeforeOrder = []byte(" ORDER BY e.epoch_secs DESC, e.nanos DESC LIMIT ?")
)

// listEntries obtém uma página de resultados (seek method or keyset pagination).
// A ordenação é inversa, o resultado mais antigo vem primeiro
// @TODO: Adicionar epochMax
func (s *storage) Entries(input *sqlog.EntriesInput) (*sqlog.Output, error) {

	var (
		levels     map[string]bool
		expr       = input.Expr
		direction  = input.Direction
		epochStart = input.EpochStart
		nanosStart = input.NanosStart
		maxResult  = input.MaxResult
	)

	if epochStart == 0 {
		epochStart = time.Now().Unix()
	}

	if len(input.Level) > 0 {
		levels = make(map[string]bool)
		for _, v := range input.Level {
			levels[v] = true
		}
	}

	buf := bytes.NewBuffer(make([]byte, 0, 128))

	if direction == "before" {
		buf.Write(sqlSeekPageBefore)
	} else {
		buf.Write(sqlSeekPageAfter)
	}
	args := []any{epochStart, epochStart, nanosStart}

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
		if compiled, err := s.config.ExprBuilder(expr); err != nil {
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
	args = append(args, min(max(maxResult, 10), 100))

	var (
		sql  = buf.String()
		list = []any{}
		dbs  []*storageDb
	)

	//fmt.Printf("[sqlog] Entries\nSQL: %s\n\nARG: %v\n", sql, args) // debug

	for _, d := range s.dbs {
		if direction == "before" {
			if d.epochStart <= epochStart {
				//  er       |
				//  ds |--------|
				//  ds |---|
				dbs = append(dbs, d)
			}
		} else {
			// from older to new
			if d.epochEnd == 0 || d.epochEnd >= epochStart {
				//  er   |
				//  ds |--------|
				//  ds     |---|
				dbs = append(dbs, d)
			}
		}
	}

	if direction == "before" {
		sort.SliceStable(dbs, func(i, j int) bool {
			return dbs[i].epochStart > dbs[j].epochStart
		})
	} else {
		sort.SliceStable(dbs, func(i, j int) bool {
			return dbs[i].epochStart < dbs[j].epochStart
		})
	}

	out := &sqlog.Output{}

	for _, db := range dbs {
		if db.isOpen() {
			if ll, err := listEntries(db, sql, args); err != nil {
				return nil, err
			} else {
				list = append(list, ll...)
			}

			if len(list) >= maxResult {
				break
			}
		} else {
			// schedule more result
			out.Scheduled = true
			out.TaskIds = s.schedule([]*storageDb{db}, func(db *storageDb, o *sqlog.Output) error {
				if list, err := listEntries(db, sql, args); err != nil {
					return err
				} else {
					o.Entries = list
					return nil
				}
			})
			break
		}
	}

	out.Entries = list
	return out, nil
}

func listEntries(db *storageDb, sql string, args []any) ([]any, error) {
	var list []any

	stm, rows, err := db.query(sql, args)
	if err != nil {
		return nil, err
	}
	defer stm.Close()
	defer rows.Close()

	for rows.Next() {
		var (
			epoch   int64
			nanos   int
			level   int
			content string
		)
		if err = rows.Scan(&epoch, &nanos, &level, &content); err != nil {
			rows.Close()
			stm.Close()
			return nil, err
		}

		list = append(list, []any{epoch, nanos, level, content})
	}

	return list, nil
}
