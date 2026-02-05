package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/tos-network/labu/labusim"
)

type VectorSuite struct {
	TestVectors []TestVector `json:"test_vectors" yaml:"test_vectors"`
}

type TestVector struct {
	Name        string                 `json:"name" yaml:"name"`
	Description string                 `json:"description" yaml:"description"`
	PreState    map[string]interface{} `json:"pre_state" yaml:"pre_state"`
	Input       struct {
		Kind    string `json:"kind" yaml:"kind"`
		WireHex string `json:"wire_hex" yaml:"wire_hex"`
	} `json:"input" yaml:"input"`
	Expected struct {
		Success     *bool  `json:"success" yaml:"success"`
		ErrorCode   *int   `json:"error_code" yaml:"error_code"`
		StateDigest string `json:"state_digest" yaml:"state_digest"`
	} `json:"expected" yaml:"expected"`
}

type ExecResult struct {
	Success     bool   `json:"success"`
	ErrorCode   int    `json:"error_code"`
	StateDigest string `json:"state_digest"`
}

func main() {
	sim := labusim.New()
	vectorDir := labusim.VectorDir()
	vectors, err := loadVectors(vectorDir)
	if err != nil {
		panic(err)
	}

	suite := labusim.Suite{
		Name:        "tos/consensus",
		Description: "Consensus conformance suite",
	}

	clients := labusim.ClientList()
	if len(clients) == 0 {
		panic("LABU_CLIENTS is empty")
	}

	if len(vectors) == 0 {
		suite.Add(labusim.TestSpec{
			Name:        "consensus/skeleton",
			Description: "No vectors found",
			Run:         func(t *labusim.T) {},
		})
		labusim.MustRunSuite(sim, suite)
		return
	}

	for _, vec := range vectors {
		vec := vec
		suite.Add(labusim.TestSpec{
			Name:        "consensus/" + vec.Name,
			Description: vec.Description,
			Run: func(t *labusim.T) {
				results := make(map[string]ExecResult)
				for _, client := range clients {
					res, err := runAgainstClient(t, client, vec)
					if err != nil {
						t.Failf("%s: %v", client, err)
						return
					}
					results[client] = res
				}
				if len(clients) == 1 {
					if err := validateExpected(vec, results[clients[0]]); err != nil {
						t.Failf("%s: %v", vec.Name, err)
					}
					return
				}
				for name, res := range results {
					if err := validateExpected(vec, res); err != nil {
						t.Failf("%s: %v", name, err)
						return
					}
				}
			},
		})
	}

	labusim.MustRunSuite(sim, suite)
}

func runAgainstClient(t *labusim.T, clientName string, vec TestVector) (ExecResult, error) {
	spec := labusim.ClientTestSpec{
		Name:        "consensus-" + vec.Name + "-" + clientName,
		Description: "execute block vector on client",
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
	if vec.Input.WireHex == "" {
		return ExecResult{}, fmt.Errorf("block vector missing wire_hex")
	}
	var execRes ExecResult
	if err := postJSON(baseURL+"/block/execute", map[string]interface{}{"wire_hex": vec.Input.WireHex}, &execRes); err != nil {
		return ExecResult{}, fmt.Errorf("block execute: %w", err)
	}
	return execRes, nil
}

func validateExpected(vec TestVector, res ExecResult) error {
	if vec.Expected.Success != nil {
		if res.Success != *vec.Expected.Success {
			return fmt.Errorf("expected success=%v, got %v", *vec.Expected.Success, res.Success)
		}
	}
	if vec.Expected.ErrorCode != nil {
		if res.ErrorCode != *vec.Expected.ErrorCode {
			return fmt.Errorf("expected error_code=%d, got %d", *vec.Expected.ErrorCode, res.ErrorCode)
		}
	}
	if vec.Expected.StateDigest != "" && res.StateDigest != "" && vec.Expected.StateDigest != res.StateDigest {
		return fmt.Errorf("expected state_digest=%s, got %s", vec.Expected.StateDigest, res.StateDigest)
	}
	return nil
}

func loadVectors(root string) ([]TestVector, error) {
	if root == "" {
		return nil, nil
	}
	var out []TestVector
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var suite VectorSuite
		if ext == ".json" {
			if err := json.Unmarshal(data, &suite); err != nil {
				return err
			}
		} else {
			if err := yaml.Unmarshal(data, &suite); err != nil {
				return err
			}
		}
		for _, vec := range suite.TestVectors {
			if vec.Input.Kind == "block" || vec.Input.Kind == "" {
				out = append(out, vec)
			}
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

func readHTTPError(r io.Reader) error {
	b, _ := io.ReadAll(r)
	if len(b) == 0 {
		return errors.New("http error")
	}
	return errors.New(string(b))
}

func bytesReader(b []byte) io.Reader {
	return bytes.NewReader(b)
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
	}
	return fmt.Errorf("health check failed")
}
