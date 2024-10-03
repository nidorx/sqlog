package sqlite

import (
	"sort"
	"sync/atomic"
	"time"

	"github.com/nidorx/sqlog"
)

const (
	task_created int32 = iota
	task_process
	task_finished
	task_canceled
)

type dbTask struct {
	db       *storageDb
	state    int32 // 0=created, 1=process, 2=finished, 3=canceled
	output   *sqlog.Output
	callback func(*storageDb, *sqlog.Output) error
}

// Result obtém o resultado de um processamento asíncrono
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

// Cancel cancela um processamento asíncrono
func (s *storage) Cancel(taskId int32) error {
	if v, loaded := s.taskMap.Load(taskId); loaded {
		task := v.(*dbTask)
		atomic.StoreInt32(&task.state, task_canceled)
		task.db.cancel(taskId)
		s.taskMap.Delete(taskId)
	}
	return nil
}

// schedule agenda a execução do callback para ser executado em cada db da lista
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

// routineScheduledTasks gerenciamentoo de tarefas agendadas no storage
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

func (s *storage) doRoutineScheduledTasks() {
	var (
		totalTasks      int32
		totalOpen       int32
		openWithTasks   []*storageDb
		openWithoutTask []*storageDb
		closedWithTasks []*storageDb
	)

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

	// executa algumas tasks nos banco de dados já abertos
	if totalTasks > 0 {

		// maximo de goroutines que pode ser criada no momento
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

	// fecha banco de dados idle
	for _, db := range s.dbs {
		if !db.live && db.lastQuerySec() > s.config.CloseIdleSec && db.closeSafe() {
			totalOpen--
			closedAnyDb = true
		}
	}

	// busca manter o limite de banco de dados abertos (sem levar em consideraçao CloseIdleSec)
	if totalOpen > s.config.MaxOpenedDB {

		// fecha banco de dados que não estão ativos
		for _, db := range openWithoutTask {
			if totalOpen > s.config.MaxOpenedDB {
				if !db.live && db.closeSafe() {
					totalOpen--
					closedAnyDb = true
				}
			}
		}

		// fecha os banco de dados com menor número de task
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

	// conecta banco de dados que possuem tarefas ativas e estão fechados
	if len(closedWithTasks) > 0 {
		sort.SliceStable(closedWithTasks, func(i, j int) bool {
			return closedWithTasks[i].tasks() < closedWithTasks[j].tasks()
		})

		if totalOpen > s.config.MaxOpenedDB {
			// abre o banco de dados com o menor número de tarefas (apenas um, para evitar starvation)
			for _, db := range closedWithTasks {
				if err := db.connect(s.config.SQLiteOptions); err == nil {
					break
				}
			}
		} else {
			// abre o máximo de banco de dados permitido
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

	if closedAnyDb {
		//
		s.mu.Lock()
		sortDbs(s.dbs)
		sortDbs(s.liveDbs)
		s.mu.Unlock()
	}
}

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
				// banco de dados fechou nesse intervalo
				if atomic.CompareAndSwapInt32(&task.state, task_process, task_created) {
					db.schedule(id, task) // agenda novamente a execução
				}
				onTask(id, true)
				return
			}

			if err := task.callback(db, task.output); err != nil {
				if !db.isOpen() {
					// banco de dados fechou nesse intervalo
					if atomic.CompareAndSwapInt32(&task.state, task_process, task_created) {
						db.schedule(id, task) // agenda novamente a execução
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
