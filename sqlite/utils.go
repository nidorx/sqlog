package sqlite

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func newDb(driver, dir, prefix string, start time.Time, maxChunkAgeSec int64) *storageDb {

	epochStart := start.Unix()
	name := fmt.Sprintf("%s_%d.db", prefix, epochStart)

	return &storageDb{
		fileDir:        dir,
		filePath:       path.Join(dir, name),
		filePrefix:     prefix,
		size:           0, // live db will updated during execution
		status:         db_closed,
		epochStart:     epochStart,
		newEpochStart:  epochStart,
		maxChunkAgeSec: maxChunkAgeSec,
		epochEnd:       0,
		driver:         driver,
	}
}

func initDbs(driver, dir, prefix string) (dbs []*storageDb, err error) {

	if err = os.MkdirAll(dir, 0755); err != nil {
		return
	}

	err = filepath.Walk(dir, func(filepath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		name := info.Name()
		if !strings.HasPrefix(name, prefix) || path.Ext(name) != ".db" {
			return nil
		}

		var (
			epochStart int64
			epochEnd   int64
		)

		epochs := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, prefix+"_"), ".db"), "_")

		epochStart, err = strconv.ParseInt(epochs[0], 10, 64)
		if err != nil {
			slog.Warn("[sqlog] invalid database name", slog.String("filepath", filepath), slog.Any("err", err))
			return nil
		}

		if len(epochs) > 1 {
			epochEnd, err = strconv.ParseInt(epochs[1], 10, 64)
			if err != nil {
				slog.Warn("[sqlog] invalid database name", slog.String("filepath", filepath), slog.Any("err", err))
				return nil
			}
		}

		dbs = append(dbs, &storageDb{
			filePath:   path.Join(dir, name),
			size:       info.Size(), // live db will updated during execution
			status:     db_closed,
			epochStart: epochStart,
			epochEnd:   epochEnd,
			driver:     driver,
		})

		return nil
	})
	if err != nil {
		err = errors.Join(errors.New("[sqlog] error initializing the storage"), err)
	}

	if len(dbs) > 1 {
		sortDbs(dbs)
	}

	return
}

func sortDbs(dbs []*storageDb) {
	sort.SliceStable(dbs, func(i, j int) bool {
		ae := dbs[i].epochEnd
		be := dbs[j].epochEnd
		if ae == 0 && be == 0 {
			return dbs[i].epochStart < dbs[j].epochStart
		}

		if be == 0 {
			return true
		}

		if ae == 0 {
			return false
		}

		return ae > be
	})
}
