package sqlog

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

var (
	sqlTicksInit = []byte(`
		WITH RECURSIVE series(idx, epoch_start, epoch_end) AS (
			SELECT ?-1, ? - ?, ? 
			UNION ALL 
			SELECT idx-1, epoch_start - ?, epoch_end - ? FROM series LIMIT ?
		)	
		SELECT 
			c.idx, 
			c.epoch_start, 
			c.epoch_end,
			COUNT(e.epoch_secs) AS count,
			COUNT(CASE WHEN e.level < 0 THEN 1 END) AS count_debug,
			COUNT(CASE WHEN e.level >= 0 AND e.level < 4 THEN 1 END) AS count_info,
			COUNT(CASE WHEN e.level >= 4 AND e.level < 8 THEN 1 END) AS count_warn,
			COUNT(CASE WHEN e.level >= 8 THEN 1 END) AS count_error
		FROM series c
		JOIN entries e ON e.epoch_secs >= c.epoch_start AND e.epoch_secs < c.epoch_end
	`)
	sqlTicksEnd = []byte(`GROUP BY c.epoch_start, c.epoch_end`)
)

// listTicks obtém as informações sobre todas as séries no intervalo
func (s *storageImpl) Ticks(input *TicksInput) (*Output, error) {

	var (
		levels      map[string]bool
		expr        = input.Expr
		epochEnd    = input.EpochEnd
		intervalSec = input.IntervalSec
		maxResult   = input.MaxResult
	)

	if epochEnd == 0 {
		epochEnd = time.Now().Unix()
	}

	if len(input.Level) > 0 {
		levels = make(map[string]bool)
		for _, v := range input.Level {
			levels[v] = true
		}
	}

	buf := bytes.NewBuffer(make([]byte, 0, 128))
	buf.Write(sqlTicksInit)

	args := []any{
		maxResult,
		epochEnd,
		intervalSec,
		epochEnd,
		intervalSec,
		intervalSec,
		maxResult,
	}

	clause := " WHERE "

	if len(levels) > 0 && len(levels) != 4 {
		buf.WriteString(clause)
		buf.WriteString(" (")

		if levels["error"] && levels["warn"] && levels["info"] {
			buf.WriteString(" e.level >= 0 ")
		} else {
			clause = ""

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

		clause = " AND "
	}

	if expr = strings.TrimSpace(expr); expr != "" {
		if compiled, err := Compile(expr, nil); err != nil {
			return nil, err
		} else if compiled.Sql != "" {
			buf.WriteString(clause)
			buf.WriteString(compiled.Sql)
			buf.WriteString(" ")
			args = append(args, compiled.Args...)
		}
	}
	buf.Write(sqlTicksEnd)

	var (
		sql         = buf.String()
		epochStart  = epochEnd - int64((intervalSec * maxResult))
		dbs         []*storageDb
		closedDbs   []*storageDb
		list        []*Tick
		tickByIndex = map[int]*Tick{}
	)

	fmt.Printf("[sqlog] Ticks\nSQL: %s\n\nARG: %v\n", sql, args) // debug

	for _, d := range s.dbs {
		if epochEnd < d.epochStart || (d.epochEnd != 0 && d.epochEnd < epochStart) {
			//  es   |---|
			//  es                 |---|
			//  ds         |----|
			continue
		}
		dbs = append(dbs, d)
	}

	for _, db := range dbs {
		if db.isOpen() {
			if ll, err := listTicks(db, sql, args); err != nil {
				return nil, err
			} else {
				for _, t := range ll {
					if o, exists := tickByIndex[t.Index]; exists {
						t.Count += o.Count
						t.Debug += o.Debug
						t.Info += o.Info
						t.Warn += o.Warn
						t.Error += o.Error
					} else {
						list = append(list, t)
					}
				}
			}
		} else {
			closedDbs = append(closedDbs, db)
		}
	}

	out := &Output{
		Ticks: list,
	}

	if len(closedDbs) > 0 {
		// schedule more result
		out.Scheduled = true
		out.TaskIds = s.schedule(closedDbs, func(db *storageDb, o *Output) error {
			if list, err := listTicks(db, sql, args); err != nil {
				return err
			} else {
				o.Ticks = list
				return nil
			}
		})
	}

	return out, nil
}

func listTicks(db *storageDb, sql string, args []any) ([]*Tick, error) {
	var list []*Tick

	stm, rows, err := db.query(sql, args)
	if err != nil {
		return nil, err
	}
	defer stm.Close()
	defer rows.Close()

	for rows.Next() {
		t := &Tick{}
		if err = rows.Scan(&t.Index, &t.Start, &t.End, &t.Count, &t.Debug, &t.Info, &t.Warn, &t.Error); err != nil {
			return nil, err
		}
		list = append(list, t)
	}

	return list, nil
}
