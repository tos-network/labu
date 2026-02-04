package results

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type SummaryResult struct {
	Pass    bool   `json:"pass"`
	Details string `json:"details"`
}

type ClientInfo struct {
	IP             string `json:"ip"`
	Name           string `json:"name"`
	InstantiatedAt string `json:"instantiatedAt"`
	LogFile        string `json:"logFile"`
}

type TestCaseResult struct {
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Start         string                `json:"start"`
	End           string                `json:"end"`
	SummaryResult SummaryResult         `json:"summaryResult"`
	ClientInfo    map[string]ClientInfo `json:"clientInfo"`
}

type SuiteResult struct {
	ID             int                      `json:"id"`
	Name           string                   `json:"name"`
	Description    string                   `json:"description"`
	ClientVersions map[string]string        `json:"clientVersions"`
	SimLog         string                   `json:"simLog"`
	TestCases      map[string]TestCaseResult `json:"testCases"`
}

type Writer struct {
	workspace string
}

func NewWriter(workspace string) *Writer {
	return &Writer{workspace: workspace}
}

func (w *Writer) WriteSuite(result SuiteResult) error {
	path := filepath.Join(w.workspace, "results", fmt.Sprintf("suite-%d.json", result.ID))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
