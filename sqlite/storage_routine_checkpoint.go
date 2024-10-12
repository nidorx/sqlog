package sqlite

import (
	"time"
)

// See https://www.sqlite.org/wal.html#ckpt
func (s *storage) routineWalCheckpoint() {
	d := time.Duration(s.config.IntervalWalCheckpointSec) * time.Second
	tick := time.NewTicker(d)
	defer tick.Stop()

	for {
		select {

		case <-tick.C:
			var liveOpenDbs []*storageDb
			s.mu.Lock()
			for _, db := range s.liveDbs {
				if db.isOpen() {
					liveOpenDbs = append(liveOpenDbs, db)
				}
			}
			s.mu.Unlock()

			for _, db := range liveOpenDbs {
				db.checkpoint(s.config.WalCheckpointMode)
			}

			tick.Reset(d)
		case <-s.quit:
			return
		}

	}
}
