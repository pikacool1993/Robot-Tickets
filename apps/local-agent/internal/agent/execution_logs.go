package agent

import (
	"sync"
	"time"
)

type executionLog struct {
	taskID  int64
	level   string
	message string
	at      time.Time
}

// One FIFO writer keeps disk I/O out of execution goroutines and s.mu.
// Producers must finish before waiting for the writer during shutdown.
type executionLogs struct {
	mu      sync.Mutex
	pending []executionLog
	running bool
	wg      sync.WaitGroup
	persist func(executionLog) error
}

func (s *Service) queueExecutionLog(id int64, level, message string) {
	q := &s.executionLogs
	q.mu.Lock()
	q.pending = append(q.pending, executionLog{id, level, message, time.Now()})
	if !q.running {
		q.running = true
		q.wg.Add(1)
		go s.writeExecutionLogs()
	}
	q.mu.Unlock()
}

func (s *Service) writeExecutionLogs() {
	q := &s.executionLogs
	defer q.wg.Done()
	for {
		q.mu.Lock()
		batch := q.pending
		q.pending = nil
		if len(batch) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()
		for _, entry := range batch {
			var err error
			if q.persist != nil {
				err = q.persist(entry)
			} else {
				err = s.tasks.AppendLogAt(entry.taskID, entry.level, entry.message, entry.at)
			}
			if err != nil && s.log != nil {
				s.log.Warn("execution task log persistence failed", "task_id", entry.taskID)
			}
		}
	}
}
