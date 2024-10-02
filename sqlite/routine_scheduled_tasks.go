package sqlite

import (
	"sort"
	"sqlog"
	"sync/atomic"
	"time"
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

// result obtém o resultado de um processamento asíncrono
func (s *storage) result(taskId int32) *sqlog.Output {
	if v, loaded := s.taskMap.Load(taskId); loaded {
		task := v.(*dbTask)
		if task.state == task_finished || task.state == task_canceled {
			s.taskMap.Delete(taskId)
			return task.output
		} else {
			return &sqlog.Output{Scheduled: true, TaskIds: []int32{taskId}}
		}
	}
	return nil
}

// cancel cancela um processamento asíncrono
func (s *storage) cancel(taskId int32) {
	if v, loaded := s.taskMap.Load(taskId); loaded {
		task := v.(*dbTask)
		atomic.StoreInt32(&task.state, task_canceled)
		task.db.cancel(taskId)
		s.taskMap.Delete(taskId)
	}
}

// routineScheduledTasks gerenciamentoo de tarefas agendadas no storage
func (s *storage) routineScheduledTasks() {
	defer close(s.shutdown)

	d := time.Duration(s.config.IntervalScheduledTasksMs)
	tick := time.NewTicker(d)
	defer tick.Stop()

	// registro das goroutines criadas para executar tarefas agendadas
	numActiveTasks := int32(0)

	for {
		select {

		case <-tick.C:
			// fecha os banco de dados não usados (exeto o atual)

			// executa consultas agendadas (quando os banco de dados estiverem disponíveis)

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

			// executa algumas tasks nos banco de dados já abertos
			if totalTasks > 0 {

				// maximo de goroutines que pode ser criada no momento
				qtMaxTasks := s.config.MaxRunningTasks - numActiveTasks
				if qtMaxTasks > 0 {
					for _, db := range openWithTasks {
						numTasks := db.tasks()
						if numTasks == 0 {
							continue
						}

						maxForThisDb := max(1, numTasks/totalTasks*qtMaxTasks)
						i := int32(0)
						db.execute(func(id int32, task *dbTask) bool {
							if id >= 0 {
								retries := 0
								var exec func()
								exec = func() {
									if atomic.CompareAndSwapInt32(&task.state, task_created, task_process) {

										// banco de dados fechou nesse intervalo, agenda execuçao
										if !db.isOpen() {
											if atomic.CompareAndSwapInt32(&task.state, task_process, task_created) {
												db.schedule(id, task)
											}
											atomic.AddInt32(&numActiveTasks, -1)
											return
										}

										if err := task.callback(db, task.output); err != nil {

											// banco de dados fechou nesse intervalo, agenda execuçao
											if !db.isOpen() {
												if atomic.CompareAndSwapInt32(&task.state, task_process, task_created) {
													db.schedule(id, task)
												}
												atomic.AddInt32(&numActiveTasks, -1)
												return
											}

											if retries < 3 {
												retries++
												go exec()
											} else {
												task.output.Error = err
												atomic.CompareAndSwapInt32(&task.state, task_process, task_finished)
												atomic.AddInt32(&numActiveTasks, -1)
											}
										} else {
											atomic.CompareAndSwapInt32(&task.state, task_process, task_finished)
											atomic.AddInt32(&numActiveTasks, -1)
										}
									}
								}
								atomic.AddInt32(&numActiveTasks, 1)
								go exec()
							}

							i++
							return i < maxForThisDb
						})
					}
				}
			}

			// fecha banco de dados idle
			for _, db := range s.dbs {
				if !db.live && db.lastQuerySec() > s.config.CloseIdleSec && db.closeSafe() {
					totalOpen--
				}
			}

			// busca manter o limite de banco de dados abertos (sem levar em consideraçao CloseIdleSec)
			if totalOpen > s.config.MaxOpenedDB {

				// fecha banco de dados que não estão ativos
				for _, db := range openWithoutTask {
					if totalOpen > s.config.MaxOpenedDB {
						if !db.live && db.closeSafe() {
							totalOpen--
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
					// abre o banco de dados com o menor número de tarefas
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

			tick.Reset(d)
		case <-s.quit:
			return
		}
	}
}
