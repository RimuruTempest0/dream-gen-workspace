package core

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

func sortTasks(tasks []Task) {
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt == tasks[j].CreatedAt {
			return tasks[i].ID < tasks[j].ID
		}
		return tasks[i].CreatedAt < tasks[j].CreatedAt
	})
}

func (s *Service) ListRecentBatches(limit int) ([]RecentBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return nil, e
	}
	sortTasks(tasks)
	batches := []RecentBatch{}
	indexes := map[string]int{}
	for _, t := range tasks {
		// Smart-generation sets are shown and downloaded on their own page.
		if t.AgentRunID != "" {
			continue
		}
		id := t.BatchID
		if id == "" {
			id = t.ID
		}
		index, ok := indexes[id]
		if !ok {
			index = len(batches)
			indexes[id] = index
			batches = append(batches, RecentBatch{ID: id, CreatedAt: t.CreatedAt, Tasks: []Task{}})
		}
		b := &batches[index]
		b.Total++
		b.ImageCount += len(t.AssetIDs)
		if t.Status == "succeeded" {
			b.Completed++
		}
		if active(t.Status) {
			b.Active++
		}
		if len(b.Tasks) < 6 {
			t.Connection = cleanConnection(t.Connection)
			t.Results = nil
			b.Tasks = append(b.Tasks, t)
		}
	}
	sort.Slice(batches, func(i, j int) bool {
		if batches[i].CreatedAt == batches[j].CreatedAt {
			return batches[i].ID > batches[j].ID
		}
		return batches[i].CreatedAt > batches[j].CreatedAt
	})
	if limit < 1 || limit > 20 {
		limit = 6
	}
	if len(batches) > limit {
		batches = batches[:limit]
	}
	return batches, nil
}

func (s *Service) AssetForExport(id string) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a Asset
	e := s.store.Get("asset", id, &a)
	return a, e
}

// Export only local originals belonging to this batch, not cached thumbnails,
// other batches, credentials or provider URLs. A failure leaves no partial ZIP.
func (s *Service) ExportBatch(batchID, path string) (int, error) {
	s.mu.Lock()
	root := s.store.Root
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		s.mu.Unlock()
		return 0, e
	}
	sortTasks(tasks)
	// A smart-generation slot can have several attempts (retry or refine); the set
	// contains only the newest attempt that has images, in the slot's original order.
	current := map[string]Task{}
	for _, t := range tasks {
		if t.AgentRunID != "" && t.PlanItemID != "" && len(t.AssetIDs) > 0 {
			current[t.AgentRunID+"/"+t.PlanItemID] = t
		}
	}
	exported := map[string]bool{}
	assets := []Asset{}
	seen := map[string]bool{}
	for _, t := range tasks {
		if t.BatchID != batchID && !(t.BatchID == "" && t.ID == batchID) {
			continue
		}
		if t.AgentRunID != "" && t.PlanItemID != "" {
			slot := t.AgentRunID + "/" + t.PlanItemID
			if exported[slot] {
				continue
			}
			exported[slot] = true
			if latest, ok := current[slot]; ok {
				t = latest
			}
		}
		for _, id := range t.AssetIDs {
			if seen[id] {
				continue
			}
			var a Asset
			if e = s.store.Get("asset", id, &a); e != nil {
				s.mu.Unlock()
				return 0, e
			}
			if a.TaskID != t.ID {
				s.mu.Unlock()
				return 0, errors.New("图片与任务归属不一致")
			}
			seen[id] = true
			assets = append(assets, a)
		}
	}
	s.mu.Unlock()
	if len(assets) == 0 {
		return 0, errors.New("这套任务还没有可下载的图片")
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".image-set-*.partial")
	if e != nil {
		return 0, e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	zw := zip.NewWriter(f)
	for i, a := range assets {
		filePath, err := owned(root, a.Path)
		if err != nil {
			zw.Close()
			return 0, err
		}
		source, err := os.Open(filePath)
		if err != nil {
			zw.Close()
			return 0, fmt.Errorf("第 %d 张原图不可读取：%w", i+1, err)
		}
		info, err := source.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxImageBytes {
			source.Close()
			zw.Close()
			return 0, errors.New("原图文件不可用或超过大小限制")
		}
		header := &zip.FileHeader{Name: fmt.Sprintf("%03d%s", i+1, filepath.Ext(a.Path)), Method: zip.Store}
		header.SetModTime(info.ModTime())
		entry, err := zw.CreateHeader(header)
		if err == nil {
			_, err = io.Copy(entry, io.LimitReader(source, MaxImageBytes+1))
		}
		source.Close()
		if err != nil {
			zw.Close()
			return 0, err
		}
	}
	if e = zw.Close(); e != nil {
		return 0, e
	}
	if e = f.Sync(); e != nil {
		return 0, e
	}
	if e = f.Close(); e != nil {
		return 0, e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return 0, e
	}
	return len(assets), nil
}
