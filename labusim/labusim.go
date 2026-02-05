package labusim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	EnvSimulator   = "LABU_SIMULATOR"
	EnvTestPattern = "LABU_TEST_PATTERN"
	EnvClients     = "LABU_CLIENTS"
	EnvVectorDir   = "LABU_VECTOR_DIR"
)

type Sim struct {
	BaseURL     string
	HTTP        *http.Client
	TestPattern *regexp.Regexp
}

func New() *Sim {
	base := os.Getenv(EnvSimulator)
	if base == "" {
		base = "http://127.0.0.1:9000"
	}
	var re *regexp.Regexp
	if p := os.Getenv(EnvTestPattern); p != "" {
		re, _ = regexp.Compile(p)
	}
	return &Sim{
		BaseURL:     base,
		HTTP:        &http.Client{Timeout: 120 * time.Second},
		TestPattern: re,
	}
}

func ClientList() []string {
	raw := os.Getenv(EnvClients)
	if raw == "" {
		return nil
	}
	parts := []string{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func VectorDir() string {
	return os.Getenv(EnvVectorDir)
}

type Suite struct {
	Name        string
	Description string
	Location    string
	Tests       []TestSpec
	ClientTests []ClientTestSpec
}

func (s *Suite) Add(t TestSpec) {
	s.Tests = append(s.Tests, t)
}

func (s *Suite) AddClient(t ClientTestSpec) {
	s.ClientTests = append(s.ClientTests, t)
}

type TestSpec struct {
	Name        string
	Description string
	Run         func(*T)
}

type ClientTestSpec struct {
	Name        string
	Description string
	Client      string
	Networks    []string
	Environment map[string]string
	Files       map[string]string
	Run         func(*T, *Client)
}

type T struct {
	sim        *Sim
	suiteID    int
	testID     int
	name       string
	description string
	failed     bool
	details    string
}

func (t *T) Fail(details string) {
	t.failed = true
	t.details = details
}

func (t *T) Failf(format string, args ...interface{}) {
	t.failed = true
	t.details = fmt.Sprintf(format, args...)
}

func (t *T) Log(details string) {
	if t.details == "" {
		t.details = details
	} else {
		t.details = t.details + "\n" + details
	}
}

func (t *T) LaunchClient(spec ClientTestSpec) (*Client, error) {
	return t.sim.launchClient(t.suiteID, t.testID, spec)
}

type Client struct {
	sim      *Sim
	SuiteID  int
	TestID   int
	ID       string
	IP       string
	Client   string
}

func (c *Client) Exec(command []string) (int, string, string, error) {
	payload := map[string]interface{}{"command": command}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/testsuite/%d/test/%d/node/%s/exec", c.sim.BaseURL, c.SuiteID, c.TestID, c.ID)
	resp, err := c.sim.HTTP.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return 1, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return 1, "", "", readError(resp.Body)
	}
	var out struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 1, "", "", err
	}
	return out.ExitCode, out.Stdout, out.Stderr, nil
}

func NewClientFromInfo(sim *Sim, suiteID, testID int, id, ip, name string) *Client {
	return &Client{
		sim:     sim,
		SuiteID: suiteID,
		TestID:  testID,
		ID:      id,
		IP:      ip,
		Client:  name,
	}
}

func MustRunSuite(sim *Sim, suite Suite) {
	if err := RunSuite(sim, suite); err != nil {
		panic(err)
	}
}

func RunSuite(sim *Sim, suite Suite) error {
	suiteID, err := sim.createSuite(suite.Name, suite.Description)
	if err != nil {
		return err
	}
	defer sim.endSuite(suiteID)

	for _, test := range suite.Tests {
		if !sim.match(test.Name) {
			continue
		}
		testID, err := sim.createTest(suiteID, test.Name, test.Description)
		if err != nil {
			return err
		}
		t := &T{sim: sim, suiteID: suiteID, testID: testID, name: test.Name, description: test.Description}
		test.Run(t)
		if err := sim.endTest(suiteID, testID, !t.failed, t.details); err != nil {
			return err
		}
	}

	for _, test := range suite.ClientTests {
		if !sim.match(test.Name) {
			continue
		}
		testID, err := sim.createTest(suiteID, test.Name, test.Description)
		if err != nil {
			return err
		}
		t := &T{sim: sim, suiteID: suiteID, testID: testID, name: test.Name, description: test.Description}
		client, err := sim.launchClient(suiteID, testID, test)
		if err != nil {
			t.Failf("client launch failed: %v", err)
			_ = sim.endTest(suiteID, testID, false, t.details)
			continue
		}
		test.Run(t, client)
		if err := sim.endTest(suiteID, testID, !t.failed, t.details); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sim) match(name string) bool {
	if s.TestPattern == nil {
		return true
	}
	return s.TestPattern.MatchString(name)
}

func (s *Sim) createSuite(name, desc string) (int, error) {
	payload := map[string]string{"name": name, "description": desc}
	body, _ := json.Marshal(payload)
	resp, err := s.HTTP.Post(s.BaseURL+"/testsuite", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return 0, readError(resp.Body)
	}
	var id int
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Sim) endSuite(id int) error {
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/testsuite/%d", s.BaseURL, id), nil)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return readError(resp.Body)
	}
	return nil
}

func (s *Sim) createTest(suiteID int, name, desc string) (int, error) {
	payload := map[string]string{"name": name, "description": desc}
	body, _ := json.Marshal(payload)
	resp, err := s.HTTP.Post(fmt.Sprintf("%s/testsuite/%d/test", s.BaseURL, suiteID), "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return 0, readError(resp.Body)
	}
	var id int
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Sim) endTest(suiteID, testID int, pass bool, details string) error {
	payload := map[string]interface{}{"pass": pass, "details": details}
	body, _ := json.Marshal(payload)
	resp, err := s.HTTP.Post(fmt.Sprintf("%s/testsuite/%d/test/%d", s.BaseURL, suiteID, testID), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return readError(resp.Body)
	}
	return nil
}

func (s *Sim) launchClient(suiteID, testID int, spec ClientTestSpec) (*Client, error) {
	applyDefaultClientFiles(&spec)
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)
	cfg := map[string]interface{}{
		"client":      spec.Client,
		"networks":    spec.Networks,
		"environment": spec.Environment,
	}
	cfgData, _ := json.Marshal(cfg)
	if err := writer.WriteField("config", string(cfgData)); err != nil {
		return nil, err
	}
	for dest, path := range spec.Files {
		part, err := writer.CreateFormFile(dest, filepath.Base(path))
		if err != nil {
			return nil, err
		}
		if err := copyToPart(part, path); err != nil {
			return nil, err
		}
	}
	writer.Close()

	url := fmt.Sprintf("%s/testsuite/%d/test/%d/node", s.BaseURL, suiteID, testID)
	resp, err := s.HTTP.Post(url, writer.FormDataContentType(), buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, readError(resp.Body)
	}
	var info struct {
		ID string `json:"id"`
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &Client{sim: s, SuiteID: suiteID, TestID: testID, ID: info.ID, IP: info.IP, Client: spec.Client}, nil
}

func applyDefaultClientFiles(spec *ClientTestSpec) {
	if spec.Environment == nil {
		spec.Environment = make(map[string]string)
	}
	if spec.Files == nil {
		spec.Files = make(map[string]string)
	}

	vectorDir := VectorDir()
	if vectorDir == "" {
		return
	}

	accountsPath := filepath.Join(vectorDir, "accounts.json")
	if _, err := os.Stat(accountsPath); err == nil {
		if _, ok := spec.Files["accounts.json"]; !ok {
			spec.Files["accounts.json"] = accountsPath
		}
		if _, ok := spec.Environment["LABU_ACCOUNTS_PATH"]; !ok {
			spec.Environment["LABU_ACCOUNTS_PATH"] = "/labu-files/accounts.json"
		}
	}

	genesisPath := filepath.Join(vectorDir, "genesis_state.json")
	if _, err := os.Stat(genesisPath); err == nil {
		if _, ok := spec.Files["genesis_state.json"]; !ok {
			spec.Files["genesis_state.json"] = genesisPath
		}
		if _, ok := spec.Environment["LABU_GENESIS_STATE_PATH"]; !ok {
			spec.Environment["LABU_GENESIS_STATE_PATH"] = "/labu-files/genesis_state.json"
		}
	}

	if matches, err := filepath.Glob(filepath.Join(vectorDir, "*.json")); err == nil {
		for _, path := range matches {
			base := filepath.Base(path)
			if base == "accounts.json" || base == "genesis_state.json" {
				continue
			}
			if _, ok := spec.Files[base]; !ok {
				spec.Files[base] = path
			}
			if base == "config.json" {
				if _, ok := spec.Environment["LABU_CONFIG_PATH"]; !ok {
					spec.Environment["LABU_CONFIG_PATH"] = "/labu-files/config.json"
				}
			}
		}
	}
}

func copyToPart(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func readError(r io.Reader) error {
	var payload struct {
		Error string `json:"error"`
	}
	b, _ := io.ReadAll(r)
	if len(b) == 0 {
		return errors.New("request failed")
	}
	if err := json.Unmarshal(b, &payload); err == nil && payload.Error != "" {
		return errors.New(payload.Error)
	}
	return errors.New(string(b))
}

func WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}
