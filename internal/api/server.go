package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/tos-network/labu/internal/controller"
	"github.com/tos-network/labu/internal/results"
)

type Server struct {
	ctrl   *controller.Controller
	result *results.Writer
}

func New(ctrl *controller.Controller, writer *results.Writer) *Server {
	return &Server{ctrl: ctrl, result: writer}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/clients", s.handleClients)
	mux.HandleFunc("/testsuite", s.handleSuite)
	mux.HandleFunc("/testsuite/", s.handleSuiteSub)
	return mux
}

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	clients := s.ctrl.ListClients()
	writeJSON(w, clients)
}

func (s *Server) handleSuite(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req controller.SuiteCreate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		id := s.ctrl.CreateSuite(req)
		writeJSON(w, id)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *Server) handleSuiteSub(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/testsuite/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	suiteID, err := strconv.Atoi(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid suite id")
		return
	}

	if len(parts) == 1 {
		if r.Method == http.MethodDelete {
			if err := s.ctrl.EndSuite(suiteID); err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, "ok")
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch parts[1] {
	case "test":
		s.handleTest(w, r, suiteID, parts[2:])
		return
	case "network":
		s.handleNetwork(w, r, suiteID, parts[2:])
		return
	default:
		writeError(w, http.StatusNotFound, "not found")
		return
	}
}

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request, suiteID int, parts []string) {
	if len(parts) == 0 {
		if r.Method == http.MethodPost {
			var req controller.TestCreate
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			id, err := s.ctrl.CreateTest(suiteID, req)
			if err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, id)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	testID, err := strconv.Atoi(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid test id")
		return
	}

	if len(parts) == 1 {
		if r.Method == http.MethodPost {
			var req controller.TestFinish
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if err := s.ctrl.EndTest(suiteID, testID, req); err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			if err := s.ctrl.SaveResults(s.result); err != nil {
				log.Printf("results write error: %v", err)
			}
			writeJSON(w, "ok")
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch parts[1] {
	case "node":
		s.handleNode(w, r, suiteID, testID, parts[2:])
		return
	default:
		writeError(w, http.StatusNotFound, "not found")
		return
	}
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request, suiteID, testID int, parts []string) {
	if len(parts) == 0 {
		if r.Method == http.MethodPost {
			cfg, files, err := controller.ParseMultipartConfig(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			info, err := s.ctrl.LaunchNode(suiteID, testID, cfg, files)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, info)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	containerID := parts[0]
	if len(parts) == 1 {
		if r.Method == http.MethodDelete {
			if err := s.ctrl.RemoveNode(containerID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, "ok")
			return
		}
		if r.Method == http.MethodGet {
			info, err := s.ctrl.NodeInfo(containerID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, info)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if parts[1] == "exec" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var payload struct {
			Command []string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		code, stdout, stderr, err := s.ctrl.DockerExec(containerID, payload.Command)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]interface{}{
			"exitCode": code,
			"stdout":   stdout,
			"stderr":   stderr,
		})
		return
	}

	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request, suiteID int, parts []string) {
	if len(parts) == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	netName := parts[0]
	if len(parts) == 1 {
		if r.Method == http.MethodPost {
			if err := s.ctrl.CreateNetwork(netName); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, "ok")
			return
		}
		if r.Method == http.MethodDelete {
			if err := s.ctrl.RemoveNetwork(netName); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, "ok")
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	containerID := parts[1]
	if r.Method == http.MethodPost {
		if err := s.ctrl.ConnectNetwork(netName, containerID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, "ok")
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.ctrl.DisconnectNetwork(netName, containerID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, "ok")
		return
	}
	if r.Method == http.MethodGet {
		ip, err := s.ctrl.NetworkIP(netName, containerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, ip)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *Server) Start(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, "{\"error\":%q}", msg)
}
