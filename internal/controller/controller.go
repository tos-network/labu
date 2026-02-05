package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/tos-network/labu/internal/docker"
	"github.com/tos-network/labu/internal/results"
	"gopkg.in/yaml.v3"
)

type Controller struct {
	workspace string
	docker    *docker.Runner
	mu        sync.Mutex
	suiteSeq  int
	testSeq   int
	suites    map[int]*Suite
	clients   map[string]ClientDef
	networks  map[string]struct{}
	results   map[int]*results.SuiteResult
	imageOverrides map[string]string
}

type Suite struct {
	ID          int
	Name        string
	Description string
	Tests       map[int]*Test
}

type Test struct {
	ID          int
	Name        string
	Description string
	Start       string
	End         string
	Pass        bool
	Details     string
	Nodes       map[string]*Node
}

type Node struct {
	ID             string
	ClientName     string
	ContainerID    string
	IP             string
	LogFile        string
	InstantiatedAt string
}

type ClientDef struct {
	Name    string                 `json:"name"`
	Version string                 `json:"version"`
	Meta    map[string]interface{} `json:"meta"`
	Dir     string                 `json:"-"`
}

type SuiteCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type TestCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type TestFinish struct {
	Pass    bool   `json:"pass"`
	Details string `json:"details"`
}

type ClientLaunchConfig struct {
	Client      string            `json:"client"`
	Networks    []string          `json:"networks"`
	Environment map[string]string `json:"environment"`
}

type NodeInfo struct {
	ID string `json:"id"`
	IP string `json:"ip"`
}

func New(workspace string, dockerRunner *docker.Runner) *Controller {
	c := &Controller{
		workspace: workspace,
		docker:    dockerRunner,
		suites:    make(map[int]*Suite),
		clients:   make(map[string]ClientDef),
		networks:  make(map[string]struct{}),
		results:   make(map[int]*results.SuiteResult),
		imageOverrides: make(map[string]string),
	}
	c.loadClients()
	return c
}

func (c *Controller) SetImageOverrides(overrides map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range overrides {
		c.imageOverrides[k] = v
	}
}

func (c *Controller) loadClients() {
	root := filepath.Join(c.workspace, "..", "clients")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		yamlPath := filepath.Join(root, name, "labu.yaml")
		meta := map[string]interface{}{}
		if b, err := os.ReadFile(yamlPath); err == nil {
			_ = yaml.Unmarshal(b, &meta)
		}
		c.clients[name] = ClientDef{
			Name: name,
			Meta: meta,
			Dir:  filepath.Join(root, name),
		}
	}
}

func (c *Controller) ListClients() []ClientDef {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ClientDef, 0, len(c.clients))
	for _, v := range c.clients {
		out = append(out, v)
	}
	return out
}

func (c *Controller) CreateSuite(req SuiteCreate) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.suiteSeq++
	s := &Suite{ID: c.suiteSeq, Name: req.Name, Description: req.Description, Tests: make(map[int]*Test)}
	c.suites[s.ID] = s
	c.results[s.ID] = &results.SuiteResult{
		ID:             s.ID,
		Name:           s.Name,
		Description:    s.Description,
		ClientVersions: map[string]string{},
		TestCases:      map[string]results.TestCaseResult{},
	}
	return s.ID
}

func (c *Controller) EndSuite(id int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.suites[id]; !ok {
		return errors.New("suite not found")
	}
	delete(c.suites, id)
	return nil
}

func (c *Controller) CreateTest(suiteID int, req TestCreate) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.suites[suiteID]
	if !ok {
		return 0, errors.New("suite not found")
	}
	c.testSeq++
	t := &Test{
		ID:          c.testSeq,
		Name:        req.Name,
		Description: req.Description,
		Start:       results.NowRFC3339(),
		Nodes:       make(map[string]*Node),
	}
	s.Tests[t.ID] = t
	return t.ID, nil
}

func (c *Controller) EndTest(suiteID, testID int, req TestFinish) error {
	c.mu.Lock()
	s, ok := c.suites[suiteID]
	if !ok {
		c.mu.Unlock()
		return errors.New("suite not found")
	}
	t, ok := s.Tests[testID]
	if !ok {
		c.mu.Unlock()
		return errors.New("test not found")
	}
	t.Pass = req.Pass
	t.Details = req.Details
	t.End = results.NowRFC3339()

	caseResult := results.TestCaseResult{
		Name:        t.Name,
		Description: t.Description,
		Start:       t.Start,
		End:         t.End,
		SummaryResult: results.SummaryResult{
			Pass:    t.Pass,
			Details: t.Details,
		},
		ClientInfo: map[string]results.ClientInfo{},
	}
	for id, node := range t.Nodes {
		caseResult.ClientInfo[id] = results.ClientInfo{
			IP:             node.IP,
			Name:           node.ClientName,
			InstantiatedAt: node.InstantiatedAt,
			LogFile:        node.LogFile,
		}
	}
	res := c.results[suiteID]
	res.TestCases[strconv.Itoa(testID)] = caseResult
	c.mu.Unlock()

	// Stop nodes after test
	for _, node := range t.Nodes {
		_ = c.docker.Remove(node.ContainerID)
	}

	return nil
}

func (c *Controller) LaunchNode(suiteID, testID int, cfg ClientLaunchConfig, files map[string]string) (NodeInfo, error) {
	c.mu.Lock()
	s, ok := c.suites[suiteID]
	if !ok {
		c.mu.Unlock()
		return NodeInfo{}, errors.New("suite not found")
	}
	t, ok := s.Tests[testID]
	if !ok {
		c.mu.Unlock()
		return NodeInfo{}, errors.New("test not found")
	}
	clientDef, ok := c.clients[cfg.Client]
	if !ok {
		c.mu.Unlock()
		return NodeInfo{}, errors.New("unknown client")
	}
	imageOverride := c.imageOverrides[cfg.Client]
	c.mu.Unlock()

	// Build image
	imageTag := imageOverride
	if imageTag == "" {
		imageTag = fmt.Sprintf("labu-client-%s", cfg.Client)
		if err := c.docker.Build(clientDef.Dir, "Dockerfile", imageTag, nil); err != nil {
			return NodeInfo{}, err
		}
	}

	// Prepare files directory
	nodeDir := filepath.Join(c.workspace, "nodes", fmt.Sprintf("suite-%d", suiteID), fmt.Sprintf("test-%d", testID))
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		return NodeInfo{}, err
	}
	for name, path := range files {
		dest := filepath.Join(nodeDir, name)
		if err := copyFile(path, dest); err != nil {
			return NodeInfo{}, err
		}
	}

	env := map[string]string{}
	for k, v := range cfg.Environment {
		env[k] = v
	}
	env["LABU_FILES_DIR"] = "/labu-files"
	if _, ok := env["LABU_STATE_DIR"]; !ok {
		env["LABU_STATE_DIR"] = "/state"
	}
	if _, ok := env["LABU_NETWORK"]; !ok {
		if len(cfg.Networks) > 0 {
			env["LABU_NETWORK"] = cfg.Networks[0]
		} else {
			env["LABU_NETWORK"] = "devnet"
		}
	}

	mounts := []string{
		fmt.Sprintf("%s:/labu-files:ro", nodeDir),
	}

	containerID, err := c.docker.Run(docker.RunConfig{
		Image:   imageTag,
		Env:     env,
		Mounts:  mounts,
		Network: "labu-net",
	})
	if err != nil {
		return NodeInfo{}, err
	}

	ip, _ := c.docker.InspectIP("labu-net", containerID)
	node := &Node{
		ID:             containerID,
		ClientName:     cfg.Client,
		ContainerID:    containerID,
		IP:             ip,
		InstantiatedAt: results.NowRFC3339(),
		LogFile:        filepath.Join("clients", cfg.Client, "client-"+containerID+".log"),
	}

	c.mu.Lock()
	t.Nodes[node.ID] = node
	c.mu.Unlock()

	return NodeInfo{ID: containerID, IP: ip}, nil
}

func (c *Controller) CreateNetwork(name string) error {
	c.mu.Lock()
	if _, ok := c.networks[name]; ok {
		c.mu.Unlock()
		return nil
	}
	c.networks[name] = struct{}{}
	c.mu.Unlock()
	return c.docker.CreateNetwork(name)
}

func (c *Controller) RemoveNetwork(name string) error {
	c.mu.Lock()
	delete(c.networks, name)
	c.mu.Unlock()
	return c.docker.RemoveNetwork(name)
}

func (c *Controller) ConnectNetwork(name, containerID string) error {
	return c.docker.ConnectNetwork(name, containerID)
}

func (c *Controller) DisconnectNetwork(name, containerID string) error {
	return c.docker.DisconnectNetwork(name, containerID)
}

func (c *Controller) NetworkIP(name, containerID string) (string, error) {
	return c.docker.InspectIP(name, containerID)
}

func (c *Controller) SaveResults(writer *results.Writer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, res := range c.results {
		if err := writer.WriteSuite(*res); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) DockerExec(containerID string, cmd []string) (int, string, string, error) {
	return c.docker.Exec(containerID, cmd)
}

func (c *Controller) SetSimLog(logFile string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, res := range c.results {
		res.SimLog = logFile
	}
}

func (c *Controller) SetClientVersions(names []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, res := range c.results {
		for _, name := range names {
			if _, ok := res.ClientVersions[name]; !ok {
				res.ClientVersions[name] = ""
			}
		}
	}
}

func (c *Controller) RemoveNode(containerID string) error {
	return c.docker.Remove(containerID)
}

func (c *Controller) NodeInfo(containerID string) (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, suite := range c.suites {
		for _, test := range suite.Tests {
			if node, ok := test.Nodes[containerID]; ok {
				return map[string]string{
					"id":   node.ID,
					"name": node.ClientName,
				}, nil
			}
		}
	}
	return map[string]string{"id": containerID}, nil
}

func parseConfig(r io.Reader) (ClientLaunchConfig, error) {
	var cfg ClientLaunchConfig
	dec := json.NewDecoder(r)
	if err := dec.Decode(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func ParseMultipartConfig(req *http.Request) (ClientLaunchConfig, map[string]string, error) {
	if err := req.ParseMultipartForm(128 << 20); err != nil {
		return ClientLaunchConfig{}, nil, err
	}
	cfgVal := req.FormValue("config")
	if cfgVal == "" {
		return ClientLaunchConfig{}, nil, errors.New("missing config")
	}
	cfg, err := parseConfig(strings.NewReader(cfgVal))
	if err != nil {
		return ClientLaunchConfig{}, nil, err
	}

	files := make(map[string]string)
	for key, fhs := range req.MultipartForm.File {
		for _, fh := range fhs {
			path, err := saveUpload(fh)
			if err != nil {
				return cfg, nil, err
			}
			files[key] = path
		}
	}
	return cfg, files, nil
}

func saveUpload(fh *multipart.FileHeader) (string, error) {
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()

	tmp, err := os.CreateTemp("", "labu-upload-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, f); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
