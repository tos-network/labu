package main

import (
	"bytes"
	"encoding/json"
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
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
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
					exit, _, stderr, err := c.Exec([]string{"curl", "-fsS", "http://127.0.0.1:8080/health"})
					if err != nil || exit != 0 {
						t.Failf("health check failed: %v %s", err, stderr)
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
					rpcURL := vec.Input.RPCURL
					if rpcURL == "" {
						rpcURL = "http://127.0.0.1:8080/json_rpc"
					}
					resp, err := callRPC(rpcURL, vec.Input.RPC)
					if err != nil {
						t.Failf("rpc call failed: %v", err)
						return
					}
					if len(vec.Expected.Response) > 0 {
						if !bytes.Equal(bytes.TrimSpace(resp), bytes.TrimSpace(vec.Expected.Response)) {
							t.Failf("rpc response mismatch: got=%s expected=%s", string(resp), string(vec.Expected.Response))
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
