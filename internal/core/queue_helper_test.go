package core

// stepNext runs the oldest waiting task synchronously to completion, as the two
// queue pools would: the generation step, then the download step for its result.
func (s *Service) stepNext() bool {
	s.mu.Lock()
	tasks, e := List[Task](s.store, "task")
	s.mu.Unlock()
	if e != nil {
		return false
	}
	for i := len(tasks) - 1; i >= 0; i-- {
		if tasks[i].Status == "queued" || tasks[i].Status == "downloading" {
			ok := s.step(tasks[i])
			var after Task
			s.mu.Lock()
			s.store.Get("task", tasks[i].ID, &after)
			s.mu.Unlock()
			if ok && tasks[i].Status == "queued" && after.Status == "downloading" {
				s.step(after)
			}
			return ok
		}
	}
	return false
}
