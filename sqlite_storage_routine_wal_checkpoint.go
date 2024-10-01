package sqlog

import (
	"time"
)

func (s *storageImpl) routineWalCheckpoint() {
	d := time.Duration(s.config.IntervalWalCheckpointSec) * time.Second
	tick := time.NewTicker(d)
	defer tick.Stop()

	for {
		select {

		case <-tick.C:
			for _, db := range s.liveDbs {
				if db.isOpen() {
					db.checkpoint()
				}
			}

			tick.Reset(d)
		case <-s.quit:
			return
		}

	}
}
