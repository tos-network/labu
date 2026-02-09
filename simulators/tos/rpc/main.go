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
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/tos-network/labu/labusim"
)

type VectorSuite struct {
	TestVectors []TestVector `json:"test_vectors" yaml:"test_vectors"`
}

type TestVector struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	PreState    map[string]interface{} `json:"pre_state" yaml:"pre_state"`
	Input       struct {
		RPC    map[string]interface{} `json:"rpc" yaml:"rpc"`
		RPCURL string                 `json:"rpc_url" yaml:"rpc_url"`
	} `json:"input" yaml:"input"`
	Expected struct {
		Response json.RawMessage `json:"response" yaml:"response"`
	} `json:"expected" yaml:"expected"`
}

func main() {
	sim := labusim.New()
	clients := labusim.ClientList()
	if len(clients) == 0 {
		panic("LABU_CLIENTS is empty")
	}

	vectorDir := labusim.VectorDir()
	vectors, err := loadVectors(vectorDir)
	if err != nil {
		panic(err)
	}

	suite := labusim.Suite{
		Name:        "tos/rpc",
		Description: "RPC conformance suite (health + vectors)",
	}

	if len(vectors) == 0 {
		for _, client := range clients {
			cname := client
			suite.AddClient(labusim.ClientTestSpec{
				Name:        fmt.Sprintf("%s/health", cname),
				Description: "Health endpoint should respond",
				Client:      cname,
				Run: func(t *labusim.T, c *labusim.Client) {
					baseURL := fmt.Sprintf("http://%s:8080", c.IP)
					if err := waitForHealth(baseURL); err != nil {
						t.Failf("health check failed: %v", err)
						return
					}
				},
			})
		}
		labusim.MustRunSuite(sim, suite)
		return
	}

	for _, vec := range vectors {
		vec := vec
		for _, client := range clients {
			cname := client
			suite.AddClient(labusim.ClientTestSpec{
				Name:        fmt.Sprintf("rpc/%s/%s", vec.Name, cname),
				Description: vec.Description,
				Client:      cname,
				Run: func(t *labusim.T, c *labusim.Client) {
					baseURL := fmt.Sprintf("http://%s:8080", c.IP)
					if err := waitForHealth(baseURL); err != nil {
						t.Failf("health check failed: %v", err)
						return
					}

					// Make vectors deterministic by resetting state and optionally loading pre_state.
					if err := postJSON(baseURL+"/state/reset", map[string]interface{}{}, nil); err != nil {
						t.Failf("state reset failed: %v", err)
						return
					}
					if vec.PreState != nil {
						if err := postJSON(baseURL+"/state/load", vec.PreState, nil); err != nil {
							t.Failf("state load failed: %v", err)
							return
						}
					}

					rpcURL := resolveRPCURL(baseURL, vec.Input.RPCURL)
					resp, err := callRPC(rpcURL, vec.Input.RPC)
					if err != nil {
						t.Failf("rpc call failed: %v", err)
						return
					}
					if len(vec.Expected.Response) > 0 {
						got, err := canonicalJSON(resp)
						if err != nil {
							t.Failf("rpc response invalid json: %v", err)
							return
						}
						exp, err := canonicalJSON(vec.Expected.Response)
						if err != nil {
							t.Failf("expected response invalid json: %v", err)
							return
						}
						if !bytes.Equal(got, exp) {
							t.Failf("rpc response mismatch: got=%s expected=%s", string(got), string(exp))
						}
					}
				},
			})
		}
	}

	labusim.MustRunSuite(sim, suite)
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
			// Only accept rpc vectors; ignore execution/consensus vectors in the same vectorDir.
			if vec.Input.RPC == nil || len(vec.Input.RPC) == 0 {
				continue
			}
			out = append(out, vec)
		}
		return nil
	})
	return out, err
}

func callRPC(url string, payload map[string]interface{}) ([]byte, error) {
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("rpc status %d: %s", resp.StatusCode, string(b))
	}
	return b, nil
}

func resolveRPCURL(baseURL, rpcURL string) string {
	if rpcURL == "" {
		return baseURL + "/json_rpc"
	}
	if strings.HasPrefix(rpcURL, "http://") || strings.HasPrefix(rpcURL, "https://") {
		return rpcURL
	}
	if strings.HasPrefix(rpcURL, "/") {
		return baseURL + rpcURL
	}
	return baseURL + "/" + rpcURL
}

func canonicalJSON(raw []byte) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func postJSON(url string, payload interface{}, out interface{}) error {
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
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

func waitForHealth(baseURL string) error {
	url := baseURL + "/health"
	for i := 0; i < 20; i++ {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode/100 == 2 {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("health check timeout: %s", url)
}
