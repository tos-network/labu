package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

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
		Kind    string                 `json:"kind" yaml:"kind"`
		WireHex string                 `json:"wire_hex" yaml:"wire_hex"`
		RPC     map[string]interface{} `json:"rpc" yaml:"rpc"`
		Tx      map[string]interface{} `json:"tx" yaml:"tx"`
	} `json:"input" yaml:"input"`
	Expected struct {
		Success     *bool                  `json:"success" yaml:"success"`
		ErrorCode   *int                   `json:"error_code" yaml:"error_code"`
		StateDigest string                 `json:"state_digest" yaml:"state_digest"`
		PostState   map[string]interface{} `json:"post_state" yaml:"post_state"`
	} `json:"expected" yaml:"expected"`
	Transaction struct {
		WireHex string `json:"wire_hex" yaml:"wire_hex"`
	} `json:"transaction" yaml:"transaction"`
}

type ExecResult struct {
	Success     bool   `json:"success"`
	ErrorCode   int    `json:"error_code"`
	StateDigest string `json:"state_digest"`
}

type ClientResult struct {
	Exec ExecResult
	Post map[string]interface{}
}

func main() {
	sim := labusim.New()
	vectorDir := labusim.VectorDir()
	if vectorDir == "" {
		panic("LABU_VECTOR_DIR is required for execution simulator")
	}

	vectors, err := loadVectors(vectorDir)
	if err != nil {
		panic(err)
	}

	suite := labusim.Suite{
		Name:        "tos/execution",
		Description: "Execution conformance suite",
	}

	clients := labusim.ClientList()
	if len(clients) == 0 {
		panic("LABU_CLIENTS is empty")
	}

	for _, vec := range vectors {
		vec := vec
		suite.Add(labusim.TestSpec{
			Name:        "execution/" + vec.File + "/" + vec.Vector.Name,
			Description: "Vector " + vec.Vector.Name,
			Run: func(t *labusim.T) {
				if err := runVectorCase(t, vec.Vector, clients); err != nil {
					t.Failf("%s: %v", vec.Vector.Name, err)
				}
			},
		})
	}

	labusim.MustRunSuite(sim, suite)
}

func runVectorCase(t *labusim.T, vec TestVector, clients []string) error {
	results := make(map[string]ClientResult)
	for _, client := range clients {
		res, err := runAgainstClient(t, client, vec)
		if err != nil {
			return fmt.Errorf("%s: %w", client, err)
		}
		results[client] = res
	}
	if len(clients) == 1 {
		if err := validateExpected(vec, results[clients[0]].Exec); err != nil {
			return err
		}
		if err := validatePostState(vec, results[clients[0]].Post); err != nil {
			return err
		}
		return nil
	}
	if err := validateExpectedForAll(vec, results); err != nil {
		return err
	}
	for _, client := range clients {
		if err := validatePostState(vec, results[client].Post); err != nil {
			return err
		}
	}
	return compareResults(results, clients[0])
}

func runAgainstClient(t *labusim.T, clientName string, vec TestVector) (ClientResult, error) {
	spec := labusim.ClientTestSpec{
		Name:        "execution-" + vec.Name + "-" + clientName,
		Description: "execute vector on client",
		Client:      clientName,
		Environment: map[string]string{},
	}

	client, err := t.LaunchClient(spec)
	if err != nil {
		return ClientResult{}, err
	}

	baseURL := fmt.Sprintf("http://%s:8080", client.IP)
	if err := waitForHealth(baseURL); err != nil {
		return ClientResult{}, fmt.Errorf("health check: %w", err)
	}

	if err := postJSON(baseURL+"/state/reset", map[string]interface{}{}, nil); err != nil {
		return ClientResult{}, fmt.Errorf("state reset: %w", err)
	}
	if vec.PreState != nil {
		if err := postJSON(baseURL+"/state/load", vec.PreState, nil); err != nil {
			return ClientResult{}, fmt.Errorf("state load: %w", err)
		}
	}
	var execRes ExecResult
	kind := vec.Input.Kind
	if kind == "" {
		kind = "tx"
	}
	wireHex := vec.Input.WireHex
	if wireHex == "" {
		wireHex = vec.Transaction.WireHex
	}
	if (kind == "tx" || kind == "tx_roundtrip") && wireHex == "" {
		return ClientResult{}, fmt.Errorf("%s vector missing wire_hex", kind)
	}
	if kind == "tx_roundtrip" {
		payload := map[string]interface{}{"wire_hex": wireHex}
		if err := postJSON(baseURL+"/tx/roundtrip", payload, &execRes); err != nil {
			return ClientResult{}, fmt.Errorf("tx roundtrip: %w", err)
		}
	} else if wireHex != "" || vec.Input.Tx != nil {
		payload := map[string]interface{}{}
		if wireHex != "" {
			payload["wire_hex"] = wireHex
		}
		if vec.Input.Tx != nil {
			payload["tx"] = vec.Input.Tx
		}
		if err := postJSON(baseURL+"/tx/execute", payload, &execRes); err != nil {
			return ClientResult{}, fmt.Errorf("tx execute: %w", err)
		}
	} else {
		if err := getJSON(baseURL+"/state/digest", &execRes); err != nil {
			return ClientResult{}, fmt.Errorf("state digest: %w", err)
		}
	}
	var post map[string]interface{}
	if vec.Expected.PostState != nil {
		if err := getJSON(baseURL+"/state/export", &post); err != nil {
			return ClientResult{}, fmt.Errorf("state export: %w", err)
		}
	}
	return ClientResult{Exec: execRes, Post: post}, nil
}

func compareResults(results map[string]ClientResult, reference string) error {
	ref, ok := results[reference]
	if !ok {
		return errors.New("missing reference client result")
	}
	for name, res := range results {
		if name == reference {
			continue
		}
		if res.Exec.ErrorCode != ref.Exec.ErrorCode {
			return fmt.Errorf("%s error_code mismatch: %d != %d", name, res.Exec.ErrorCode, ref.Exec.ErrorCode)
		}
		if res.Exec.Success != ref.Exec.Success {
			return fmt.Errorf("%s success mismatch: %v != %v", name, res.Exec.Success, ref.Exec.Success)
		}
		if res.Exec.StateDigest != "" && ref.Exec.StateDigest != "" && res.Exec.StateDigest != ref.Exec.StateDigest {
			return fmt.Errorf("%s state_digest mismatch", name)
		}
	}
	return nil
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

func validateExpectedForAll(vec TestVector, results map[string]ClientResult) error {
	for name, res := range results {
		if err := validateExpected(vec, res.Exec); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func validatePostState(vec TestVector, actual map[string]interface{}) error {
	if vec.Expected.PostState == nil {
		return nil
	}
	if actual == nil {
		return fmt.Errorf("state export missing for post_state validation")
	}
	return comparePostState(vec.Expected.PostState, actual)
}

func comparePostState(expected map[string]interface{}, actual map[string]interface{}) error {
	expGlobal, _ := expected["global_state"].(map[string]interface{})
	actGlobal, _ := actual["global_state"].(map[string]interface{})
	for _, field := range []string{"total_supply", "total_burned", "total_energy", "block_height", "timestamp"} {
		if expGlobal != nil {
			if v, ok := expGlobal[field]; ok {
				if actGlobal == nil || fmt.Sprint(actGlobal[field]) != fmt.Sprint(v) {
					return fmt.Errorf("global_state.%s mismatch: expected=%v got=%v", field, v, actGlobal[field])
				}
			}
		}
	}

	expAccounts := normalizeAccounts(expected["accounts"])
	actAccounts := normalizeAccounts(actual["accounts"])
	for addr, exp := range expAccounts {
		act, ok := actAccounts[addr]
		if !ok {
			return fmt.Errorf("missing account %s in actual state", addr)
		}
		for _, field := range []string{"balance", "nonce", "frozen", "energy", "flags", "data"} {
			if v, ok := exp[field]; ok {
				if fmt.Sprint(act[field]) != fmt.Sprint(v) {
					return fmt.Errorf("account %s %s mismatch: expected=%v got=%v", addr, field, v, act[field])
				}
			}
		}
	}
	return nil
}

func normalizeAccounts(raw interface{}) map[string]map[string]interface{} {
	out := make(map[string]map[string]interface{})
	list, ok := raw.([]interface{})
	if !ok {
		return out
	}
	var addrs []string
	for _, item := range list {
		obj, _ := item.(map[string]interface{})
		addr, _ := obj["address"].(string)
		if addr == "" {
			continue
		}
		out[addr] = obj
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	return out
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
			// Skip non-execution vectors that use `input.rpc` (handled by the rpc simulator).
			if vec.Input.RPC != nil && len(vec.Input.RPC) > 0 {
				continue
			}
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
