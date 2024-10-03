package sqlite

import (
	"log/slog"
	"time"
)

func (s *storage) routineSizeCheck() {
	d := time.Duration(s.config.IntervalSizeCheckSec) * time.Second
	tick := time.NewTicker(d)
	defer tick.Stop()

	for {
		select {

		case <-tick.C:

			// archiving dbs
			liveDb := s.liveDbs[len(s.liveDbs)-1]
			if liveDb.size > int64(s.config.MaxFilesizeMB)*1000000 {
				nextStart := time.Now().Add(time.Duration(s.config.IntervalSizeCheckSec * 2 * int32(time.Second)))
				ndb := newDb(s.config.Dir, s.config.Prefix, nextStart, s.config.MaxChunkAgeSec)
				if err := ndb.connect(s.config.SQLiteOptions); err != nil {
					slog.Warn(
						"[sqlog] error creating live database",
						slog.String("file", ndb.filePath),
						slog.Any("error", err),
					)
				} else {
					s.mu.Lock()
					s.liveDbs = append(s.liveDbs, ndb)
					s.mu.Unlock()
				}
			}

			// update live-dbs size
			for _, db := range s.liveDbs {
				db.updateSize()
			}

			totalSizeBytes := int64(0)
			for _, db := range s.dbs {
				totalSizeBytes += db.size
			}

			if totalSizeBytes > int64(s.config.TotalSizeCapMB)*1000000 {
				if olderDb := s.dbs[0]; !olderDb.live {
					olderDb.remove()
					s.mu.Lock()
					s.dbs = s.dbs[1:]
					s.mu.Unlock()
				}
			}

			tick.Reset(d)

			// @TODO: compress DB
		case <-s.quit:
			return
		}

	}
}
