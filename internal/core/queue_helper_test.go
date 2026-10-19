package core

// stepNext runs the oldest waiting task synchronously, as the queue would.
func (s *Service) stepNext() bool {
	s.mu.Lock()
	tasks, e := List[Task](s.store, "task")
	s.mu.Unlock()
	if e != nil {
		return false
	}
	for i := len(tasks) - 1; i >= 0; i-- {
		if tasks[i].Status == "queued" || tasks[i].Status == "downloading" {
			return s.step(tasks[i])
		}
	}
	return false
}
