package sqlog

import (
	"bytes"
	"strings"
	"time"
)

type tickModel struct {
	Index int   `json:"index"`
	Start int64 `json:"epoch_start"`
	End   int64 `json:"epoch_end"`
	Count int64 `json:"count"`
	Debug int64 `json:"debug"`
	Info  int64 `json:"info"`
	Warn  int64 `json:"warn"`
	Error int64 `json:"error"`
}

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
	sqlTicksEnd = []byte(` GROUP BY c.epoch_start, c.epoch_end`)
)

// listTicks obtém as informações sobre todas as séries no intervalo
func (s *storageImpl) listTicks(expr string, levels map[string]bool, epochStart int64, intervalSec, maxResult int) ([]*tickModel, error) {

	if epochStart == 0 {
		epochStart = time.Now().UTC().Unix()
	}

	buf := bytes.NewBuffer(make([]byte, 0, 128))
	buf.Write(sqlTicksInit)

	args := []any{
		maxResult,
		epochStart,
		intervalSec,
		epochStart,
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
		if compiled, err := compileExpr(expr); err != nil {
			return nil, err
		} else if compiled.Sql != "" {
			buf.WriteString(clause)
			buf.WriteString(compiled.Sql)
			args = append(args, compiled.Args...)
		}
	}
	buf.Write(sqlTicksEnd)

	sql := buf.String()
	println(sql)

	stm, err := s.db.Prepare(sql)
	if err != nil {
		return nil, err
	}
	defer stm.Close()

	rows, err := stm.Query(args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*tickModel

	for rows.Next() {
		it := &tickModel{}
		if err = rows.Scan(&it.Index, &it.Start, &it.End, &it.Count, &it.Debug, &it.Info, &it.Warn, &it.Error); err != nil {
			return nil, err
		}

		list = append(list, it)
	}

	return list, nil
}
