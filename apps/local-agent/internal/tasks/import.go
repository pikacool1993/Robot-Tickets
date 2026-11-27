package tasks

import (
	"errors"
	"fmt"
	"strings"
)

func (s *Service) ImportFingerprint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fingerprint
}

// ImportConfigured commits all selected rows together, so a failed save cannot
// leave half-configured tasks or allocate IDs which the caller cannot recover.
func (s *Service) ImportConfigured(configs []Task, fingerprint string) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configError != "" || s.fingerprint != fingerprint {
		return nil, errors.New("座位目录已变化，请重新预览")
	}
	next := s.clone()
	ids := make([]int64, 0, len(configs))
	for _, config := range configs {
		if next.NextID >= 9007199254740991 {
			return nil, errors.New("任务编号已达到上限")
		}
		config.ID = next.NextID
		config.Selected = append([]string{}, config.Selected...)
		config.ImportNames = append([]string{}, config.ImportNames...)
		config.Fingerprint = s.fingerprint
		config.SeatValid = true
		addLog(&config, "success", "CSV 导入任务，等待授权；预定观演人："+strings.Join(config.ImportNames, " "))
		next.NextID++
		next.Tasks = append(next.Tasks, config)
		ids = append(ids, config.ID)
	}
	if err := s.commit(next); err != nil {
		return nil, err
	}
	return ids, nil
}

func importSelectionComplete(t Task, b BusinessData) bool {
	if t.ImportCount == 0 {
		return true
	}
	if len(b.Selected) != t.ImportCount {
		return false
	}
	available := map[string]bool{}
	for _, p := range b.People {
		available[p.ID] = p.Available()
	}
	seen := map[string]bool{}
	for _, id := range b.Selected {
		if !available[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return b.ImportConfirmed
}

func matchImportedPeople(t Task, b *BusinessData) {
	if t.ImportCount == 0 || !b.Usable {
		return
	}
	if importSelectionComplete(t, *b) {
		return
	}
	b.Selected = nil
	b.ImportConfirmed = false
	used := map[string]bool{}
	for _, name := range t.ImportNames {
		var matches []string
		for _, p := range b.People {
			if p.Available() && p.Name == name {
				matches = append(matches, p.ID)
			}
		}
		if len(matches) == 1 && !used[matches[0]] {
			b.Selected = append(b.Selected, matches[0])
			used[matches[0]] = true
		}
	}
	b.ImportConfirmed = len(b.Selected) == t.ImportCount
}

func importedIssue(t Task, b BusinessData) string {
	if t.ImportCount == 0 || importSelectionComplete(t, b) {
		return ""
	}
	issues := []string{}
	for _, name := range t.ImportNames {
		count := 0
		for _, p := range b.People {
			if p.Available() && p.Name == name {
				count++
			}
		}
		if count == 0 {
			issues = append(issues, name+"未找到")
		}
		if count > 1 {
			issues = append(issues, name+"存在同名")
		}
	}
	if len(issues) > 0 {
		return "观演人待确认：" + strings.Join(issues, "；")
	}
	return fmt.Sprintf("观演人待确认：请选定全部 %d 位预定观演人", t.ImportCount)
}
