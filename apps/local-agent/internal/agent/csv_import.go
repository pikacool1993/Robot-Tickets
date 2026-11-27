package agent

import (
	"errors"
	"fmt"
	"strings"

	"robot/apps/local-agent/internal/taskcsv"
	"robot/apps/local-agent/internal/tasks"
)

type CSVPreview struct {
	Rows    []taskcsv.Row `json:"rows"`
	Created int           `json:"created"`
}
type csvImportState struct {
	document    *taskcsv.Document
	fingerprint string
	created     int
}

func (s *Service) PreviewTaskCSV(raw []byte) (CSVPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return CSVPreview{}, err
	}
	if strings.TrimSpace(s.state.ClientNumber) == "" {
		return CSVPreview{}, errors.New("客户端编号不可用，请重新登录后导入")
	}
	document, err := taskcsv.Parse(raw)
	if err != nil {
		return CSVPreview{}, err
	}
	document.ClientNumber = s.state.ClientNumber
	view := s.tasks.Snapshot()
	if view.ConfigError != "" {
		return CSVPreview{}, errors.New(view.ConfigError)
	}
	s.csvImport = &csvImportState{document: document, fingerprint: s.tasks.ImportFingerprint()}
	return CSVPreview{Rows: document.Preview(view)}, nil
}
func (s *Service) CommitTaskCSV(indices, confirmedDuplicates []int) (CSVPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return CSVPreview{}, err
	}
	batch := s.csvImport
	if batch == nil {
		return CSVPreview{}, errors.New("请先选择 CSV 并预览")
	}
	view := s.tasks.Snapshot()
	rows := batch.document.Preview(view)
	confirmed := map[int]bool{}
	for _, index := range confirmedDuplicates {
		confirmed[index] = true
	}
	selected := map[int]bool{}
	configs := make([]tasks.Task, 0, len(indices))
	for _, index := range indices {
		if index < 0 || index >= len(rows) || selected[index] {
			return CSVPreview{}, errors.New("导入行选择无效，请重新预览")
		}
		row := rows[index]
		if row.Status != "ready" && !(row.Status == "duplicate" && confirmed[index]) {
			return CSVPreview{}, fmt.Errorf("第 %d 条记录状态已变化，请重新预览或确认重复", row.Line)
		}
		selected[index] = true
		configs = append(configs, row.Config)
	}
	if len(configs) == 0 {
		return CSVPreview{}, errors.New("请选择要创建的记录")
	}
	ids, err := s.tasks.ImportConfigured(configs, batch.fingerprint)
	if err != nil {
		return CSVPreview{}, err
	}
	for i, index := range indices {
		batch.document.Records[index][batch.document.IDColumn] = fmt.Sprintf("%s-%d", batch.document.ClientNumber, ids[i])
	}
	batch.created += len(ids)
	return CSVPreview{Rows: batch.document.Preview(s.tasks.Snapshot()), Created: batch.created}, nil
}
func (s *Service) TaskCSVForExport() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return nil, err
	}
	if s.csvImport == nil {
		return nil, errors.New("请先导入 CSV")
	}
	return s.csvImport.document.Export()
}
func (s *Service) GetTaskCSVPreview() (CSVPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return CSVPreview{}, err
	}
	if s.csvImport == nil {
		return CSVPreview{Rows: []taskcsv.Row{}}, nil
	}
	s.csvImport.fingerprint = s.tasks.ImportFingerprint()
	return CSVPreview{Rows: s.csvImport.document.Preview(s.tasks.Snapshot()), Created: s.csvImport.created}, nil
}
