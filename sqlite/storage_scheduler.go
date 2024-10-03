package sqlite

import (
	"sort"
	"sync/atomic"
	"time"

	"github.com/nidorx/sqlog"
)

const (
	task_created  int32 = iota // Task has been created
	task_process               // Task is being processed
	task_finished              // Task has been completed
	task_canceled              // Task has been canceled
)

type dbTask struct {
	db       *storageDb                            // Reference to the storage database associated with the task
	state    int32                                 // Task state: 0=created, 1=processing, 2=finished, 3=canceled
	output   *sqlog.Output                         // Output of the task
	callback func(*storageDb, *sqlog.Output) error // Callback function to execute the task logic
}

// Result retrieves the result of an asynchronous task processing.
// If the task has finished or has been canceled, it returns the task output and removes the task from taskMap.
// Otherwise, it returns a status indicating that the task is still scheduled.
func (s *storage) Result(taskId int32) (*sqlog.Output, error) {
	if v, loaded := s.taskMap.Load(taskId); loaded {
		task := v.(*dbTask)
		if task.state == task_finished || task.state == task_canceled {
			s.taskMap.Delete(taskId)
			return task.output, nil
		} else {
			return &sqlog.Output{Scheduled: true, TaskIds: []int32{taskId}}, nil
		}
	}
	return nil, nil
}

// Cancel aborts an asynchronous task processing.
func (s *storage) Cancel(taskId int32) error {
	if v, loaded := s.taskMap.Load(taskId); loaded {
		task := v.(*dbTask)
		atomic.StoreInt32(&task.state, task_canceled)
		task.db.cancel(taskId)
		s.taskMap.Delete(taskId)
	}
	return nil
}

// schedule creates and schedules a task for each database in the list.
// The provided callback will be executed for each scheduled task.
func (s *storage) schedule(dbs []*storageDb, callback func(*storageDb, *sqlog.Output) error) (taskIds []int32) {
	for _, db := range dbs {
		id := atomic.AddInt32(&s.taskIdSeq, 1)
		task := &dbTask{callback: callback, output: &sqlog.Output{}}
		s.taskMap.Store(id, task)
		db.schedule(id, task)
		taskIds = append(taskIds, id)
	}
	return
}

// routineScheduledTasks manages the scheduled tasks processing loop.
// It runs on a separate goroutine, executing tasks at regular intervals.
func (s *storage) routineScheduledTasks() {
	defer close(s.shutdown)

	d := time.Duration(s.config.IntervalScheduledTasksMs)
	tick := time.NewTicker(d)
	defer tick.Stop()

	for {
		select {

		case <-tick.C:
			s.doRoutineScheduledTasks()

			tick.Reset(d)
		case <-s.quit:
			return
		}
	}
}

// doRoutineScheduledTasks processes tasks in the databases.
// It also manages the lifecycle of databases by closing idle ones
// and ensuring the right number of databases are open.
func (s *storage) doRoutineScheduledTasks() {
	var (
		totalTasks      int32
		totalOpen       int32
		openWithTasks   []*storageDb
		openWithoutTask []*storageDb
		closedWithTasks []*storageDb
	)

	// Gather information about tasks and databases
	for _, db := range s.dbs {
		tasks := db.tasks()
		totalTasks += tasks
		if db.isOpen() {
			totalOpen++
			if tasks > 0 {
				openWithTasks = append(openWithTasks, db)
			} else {
				openWithoutTask = append(openWithoutTask, db)
			}
		} else if tasks > 0 {
			closedWithTasks = append(closedWithTasks, db)
		}
	}

	closedAnyDb := false

	// Process tasks in the currently open databases
	if totalTasks > 0 {

		// Maximum number of tasks that can be processed in parallel
		qtMaxTasks := s.config.MaxRunningTasks - s.numActiveTasks
		if qtMaxTasks > 0 {

			onDbTask := func(taskId int32, complete bool) {
				if complete {
					// @TODO: remove result from taskMap after n seconds
					atomic.AddInt32(&s.numActiveTasks, -1)
				} else {
					atomic.AddInt32(&s.numActiveTasks, 1)
				}
			}

			for _, db := range openWithTasks {
				numTasks := db.tasks()
				if numTasks == 0 {
					continue
				}
				maxForThisDb := max(1, numTasks/totalTasks*qtMaxTasks)
				s.executeDbTasks(db, maxForThisDb, onDbTask)
			}
		}
	}

	// Close idle databases
	for _, db := range s.dbs {
		if !db.live && db.lastQuerySec() > s.config.CloseIdleSec && db.closeSafe() {
			totalOpen--
			closedAnyDb = true
		}
	}

	// Ensure the number of open databases is within the limit
	if totalOpen > s.config.MaxOpenedDB {

		// Close databases that have no tasks
		for _, db := range openWithoutTask {
			if totalOpen > s.config.MaxOpenedDB {
				if !db.live && db.closeSafe() {
					totalOpen--
					closedAnyDb = true
				}
			}
		}

		// Close databases with the fewest tasks
		if totalOpen > s.config.MaxOpenedDB {
			sort.SliceStable(openWithTasks, func(i, j int) bool {
				return openWithTasks[i].tasks() < openWithTasks[j].tasks()
			})
			for _, db := range openWithTasks {
				if totalOpen > s.config.MaxOpenedDB {
					if !db.live && db.closeSafe() {
						totalOpen--
						closedAnyDb = true
					}
				}
			}
		}
	}

	// Open closed databases that have pending tasks
	if len(closedWithTasks) > 0 {
		sort.SliceStable(closedWithTasks, func(i, j int) bool {
			return closedWithTasks[i].tasks() < closedWithTasks[j].tasks()
		})

		if totalOpen > s.config.MaxOpenedDB {
			// Open the database with the fewest tasks
			for _, db := range closedWithTasks {
				if err := db.connect(s.config.SQLiteOptions); err == nil {
					break
				}
			}
		} else {
			// Open as many databases as allowed
			for _, db := range closedWithTasks {
				if totalOpen > s.config.MaxOpenedDB {
					break
				}
				if err := db.connect(s.config.SQLiteOptions); err == nil {
					totalOpen++
				}
			}
		}
	}

	// If any database was closed, re-sort the database lists
	if closedAnyDb {
		s.mu.Lock()
		sortDbs(s.dbs)
		sortDbs(s.liveDbs)
		s.mu.Unlock()
	}
}

// executeDbTasks executes a set number of tasks for the given database.
// Tasks are executed asynchronously, and the task completion is managed via the onTask callback.
func (s *storage) executeDbTasks(db *storageDb, maxForThisDb int32, onTask func(taskId int32, complete bool)) {
	i := int32(0)
	db.execute(func(id int32, task *dbTask) (stops bool) {
		i++
		stops = i < maxForThisDb
		if id < 0 {
			return
		}

		retries := 0
		var exec func()
		exec = func() {
			if !atomic.CompareAndSwapInt32(&task.state, task_created, task_process) {
				onTask(id, true)
				return
			}

			if !db.isOpen() {
				// If the database closed during the task execution
				if atomic.CompareAndSwapInt32(&task.state, task_process, task_created) {
					db.schedule(id, task) // Reschedule the task
				}
				onTask(id, true)
				return
			}

			if err := task.callback(db, task.output); err != nil {
				if !db.isOpen() {
					// If the database closed during task execution
					if atomic.CompareAndSwapInt32(&task.state, task_process, task_created) {
						db.schedule(id, task) // Reschedule the task
					}
					onTask(id, true)
					return
				}

				if retries < 3 {
					retries++
					go exec()
				} else {
					task.output.Error = err
					atomic.CompareAndSwapInt32(&task.state, task_process, task_finished)
					onTask(id, true)
				}
			} else {
				atomic.CompareAndSwapInt32(&task.state, task_process, task_finished)
				onTask(id, true)
			}
		}
		onTask(id, false)

		go exec()

		return
	})
}
