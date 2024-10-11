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
			s.doRoutineSizeCheck()
			tick.Reset(d)

			// @TODO: compress DB
		case <-s.quit:
			return
		}

	}
}

func (s *storage) doRoutineSizeCheck() {
	// archiving dbs
	if s.liveDbs[len(s.liveDbs)-1].size > int64(s.config.MaxFilesizeMB)*1000000 {
		nextStart := time.Now().Add(time.Duration(s.config.IntervalSizeCheckSec * 2 * int32(time.Second)))
		ndb := newDb(s.config.Dir, s.config.Prefix, nextStart, s.config.MaxChunkAgeSec)
		ndb.live = true
		if err := ndb.connect(s.config.SQLiteOptions); err != nil {
			slog.Warn(
				"[sqlog] error creating live database",
				slog.String("file", ndb.filePath),
				slog.Any("error", err),
			)
		} else {
			s.mu.Lock()
			s.dbs = append(s.dbs, ndb)
			s.liveDbs = append(s.liveDbs, ndb)
			s.mu.Unlock()
		}
	}

	// update live-dbs
	var (
		maxLiveEpochEnd = time.Now().Unix() - s.config.MaxChunkAgeSec
		liveDbsValid    []*storageDb
		liveDbsInvalid  []*storageDb
	)
	// maxChunkAge := d.epochStart - d.maxChunkAgeSec
	for _, d := range s.liveDbs {
		d.updateSize()

		if d.epochEnd == 0 || d.epochEnd >= maxLiveEpochEnd {
			liveDbsValid = append(liveDbsValid, d)
		} else {
			liveDbsInvalid = append(liveDbsInvalid, d)
		}
	}

	if len(liveDbsValid) > 0 && len(liveDbsValid) != len(s.liveDbs) && len(s.liveDbs) > 1 {
		s.mu.Lock()
		s.liveDbs = liveDbsValid
		for _, d := range liveDbsInvalid {
			d.live = false // can be closed
		}
		s.mu.Unlock()
	}

	totalSizeBytes := int64(0)
	for _, db := range s.dbs {
		totalSizeBytes += db.size
	}

	if totalSizeBytes > int64(s.config.MaxSizeTotalMB)*1000000 {
		if olderDb := s.dbs[0]; !olderDb.live {
			olderDb.remove()
			s.mu.Lock()
			s.dbs = s.dbs[1:]
			s.mu.Unlock()
		}
	}
}
