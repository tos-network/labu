package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/tos-network/lab/labsim"
)

type VectorSuite struct {
	TestVectors []TestVector `yaml:"test_vectors"`
}

type TestVector struct {
	Name       string                 `yaml:"name"`
	PreState   map[string]interface{} `yaml:"pre_state"`
	Transaction struct {
		WireHex string `yaml:"wire_hex"`
	} `yaml:"transaction"`
}

type ExecResult struct {
	Success     bool   `json:"success"`
	ErrorCode   int    `json:"error_code"`
	StateDigest string `json:"state_digest"`
}

func main() {
	sim := labsim.New()
	vectorDir := labsim.VectorDir()
	if vectorDir == "" {
		panic("LAB_VECTOR_DIR is required for execution simulator")
	}

	vectors, err := loadVectors(vectorDir)
	if err != nil {
		panic(err)
	}

	suite := labsim.Suite{
		Name:        "tos/execution",
		Description: "Execution conformance suite",
	}

	clients := labsim.ClientList()
	if len(clients) == 0 {
		panic("LAB_CLIENTS is empty")
	}

	for _, vec := range vectors {
		vec := vec
		suite.Add(labsim.TestSpec{
			Name:        "execution/" + vec.File + "/" + vec.Vector.Name,
			Description: "Vector " + vec.Vector.Name,
			Run: func(t *labsim.T) {
				if err := runVectorCase(t, vec.Vector, clients); err != nil {
					t.Failf("%s: %v", vec.Vector.Name, err)
				}
			},
		})
	}

	labsim.MustRunSuite(sim, suite)
}

func runVectorCase(t *labsim.T, vec TestVector, clients []string) error {
	results := make(map[string]ExecResult)
	for _, client := range clients {
		res, err := runAgainstClient(t, client, vec)
		if err != nil {
			return fmt.Errorf("%s: %w", client, err)
		}
		results[client] = res
	}
	return compareResults(results, clients[0])
}

func runAgainstClient(t *labsim.T, clientName string, vec TestVector) (ExecResult, error) {
	spec := labsim.ClientTestSpec{
		Name:        "execution-" + vec.Name + "-" + clientName,
		Description: "execute vector on client",
		Client:      clientName,
		Environment: map[string]string{},
	}

	client, err := t.LaunchClient(spec)
	if err != nil {
		return ExecResult{}, err
	}

	baseURL := fmt.Sprintf("http://%s:8080", client.IP)
	if err := waitForHealth(baseURL); err != nil {
		return ExecResult{}, fmt.Errorf("health check: %w", err)
	}

	if err := postJSON(baseURL+"/state/reset", map[string]interface{}{}, nil); err != nil {
		return ExecResult{}, fmt.Errorf("state reset: %w", err)
	}
	if vec.PreState != nil {
		if err := postJSON(baseURL+"/state/load", vec.PreState, nil); err != nil {
			return ExecResult{}, fmt.Errorf("state load: %w", err)
		}
	}
	var execRes ExecResult
	if vec.Transaction.WireHex != "" {
		if err := postJSON(baseURL+"/tx/execute", map[string]interface{}{"wire_hex": vec.Transaction.WireHex}, &execRes); err != nil {
			return ExecResult{}, fmt.Errorf("tx execute: %w", err)
		}
	} else {
		if err := getJSON(baseURL+"/state/digest", &execRes); err != nil {
			return ExecResult{}, fmt.Errorf("state digest: %w", err)
		}
	}
	return execRes, nil
}

func compareResults(results map[string]ExecResult, reference string) error {
	ref, ok := results[reference]
	if !ok {
		return errors.New("missing reference client result")
	}
	for name, res := range results {
		if name == reference {
			continue
		}
		if res.ErrorCode != ref.ErrorCode {
			return fmt.Errorf("%s error_code mismatch: %d != %d", name, res.ErrorCode, ref.ErrorCode)
		}
		if res.Success != ref.Success {
			return fmt.Errorf("%s success mismatch: %v != %v", name, res.Success, ref.Success)
		}
		if res.StateDigest != "" && ref.StateDigest != "" && res.StateDigest != ref.StateDigest {
			return fmt.Errorf("%s state_digest mismatch", name)
		}
	}
	return nil
}

type NamedVector struct {
	File   string
	Vector TestVector
}

func loadVectors(root string) ([]NamedVector, error) {
	var out []NamedVector
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var suite VectorSuite
		if err := yaml.Unmarshal(data, &suite); err != nil {
			return err
		}
		for _, vec := range suite.TestVectors {
			out = append(out, NamedVector{
				File:   filepath.Base(path),
				Vector: vec,
			})
		}
		return nil
	})
	return out, err
}

func postJSON(url string, payload interface{}, out interface{}) error {
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytesReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return readHTTPError(resp.Body)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func getJSON(url string, out interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return readHTTPError(resp.Body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func readHTTPError(r io.Reader) error {
	b, _ := io.ReadAll(r)
	if len(b) == 0 {
		return errors.New("http error")
	}
	return errors.New(string(b))
}

func waitForHealth(baseURL string) error {
	url := baseURL + "/health"
	for i := 0; i < 20; i++ {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("timeout waiting for /health")
}

func bytesReader(b []byte) io.Reader {
	return &byteReader{b: b}
}

type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
