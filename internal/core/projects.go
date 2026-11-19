package core

// Projects and prompt templates.

import (
	"strings"
)

func (s *Service) ListProjects() ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return List[Project](s.store, "project")
}
func (s *Service) SaveProject(p Project) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return p, s.fail("data.project.name")
	}
	if p.ID == "" {
		p.ID = NewID()
		p.CreatedAt = Now()
	}
	return p, s.store.Put("project", p.ID, p)
}
func (s *Service) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	assets, e := List[Asset](s.store, "asset")
	if e != nil {
		return e
	}
	for _, a := range assets {
		if a.ProjectID == id {
			a.ProjectID = ""
			if e = s.store.Put("asset", a.ID, a); e != nil {
				return e
			}
		}
	}
	return s.store.Delete("project", id)
}
func (s *Service) ListTemplates() ([]Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return List[Template](s.store, "template")
}
func (s *Service) SaveTemplate(t Template) (Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Prompt) == "" {
		return t, s.fail("data.template.required")
	}
	if t.ID == "" {
		t.ID = NewID()
	}
	return t, s.store.Put("template", t.ID, t)
}
func (s *Service) DeleteTemplate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Delete("template", id)
}
