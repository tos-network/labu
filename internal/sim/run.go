package sim

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tos-network/lab/internal/api"
	"github.com/tos-network/lab/internal/controller"
	"github.com/tos-network/lab/internal/docker"
	"github.com/tos-network/lab/internal/results"
)

type Options struct {
	Simulator      string
	Clients        []string
	SimulatorImage string
	ClientImages   map[string]string
	VectorsDir     string
	LimitPattern   string
	Parallelism    int
	RandomSeed     int64
	LogLevel       int
	Workspace      string
	Controller     *controller.Controller
	ResultWriter   *results.Writer
	DockerRunner   *docker.Runner
}

func Run(opts Options) error {
	if opts.RandomSeed == 0 {
		opts.RandomSeed = time.Now().UnixNano()
	}
	rand.Seed(opts.RandomSeed)

	if err := opts.DockerRunner.CreateNetwork("lab-net"); err != nil {
		return err
	}

	opts.Controller.SetImageOverrides(opts.ClientImages)

	server := api.New(opts.Controller, opts.ResultWriter)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := ln.Addr().String()

	httpServer := &http.Server{Handler: server.Handler()}
	go func() {
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("api server error: %v", err)
		}
	}()

	simImage := opts.SimulatorImage
	if simImage == "" {
		simImage = fmt.Sprintf("lab-sim-%s", sanitize(opts.Simulator))
		simDir := filepath.Join(opts.Workspace, "..", "simulators", opts.Simulator)
		ctxDir, dockerfile := resolveBuildContext(simDir)
		if err := opts.DockerRunner.Build(ctxDir, dockerfile, simImage, nil); err != nil {
			return err
		}
	}

	// Pre-build client images (best-effort)
	successful := 0
	for _, client := range opts.Clients {
		if img, ok := opts.ClientImages[client]; ok && img != "" {
			successful++
			continue
		}
		clientDir := filepath.Join(opts.Workspace, "..", "clients", client)
		imageTag := fmt.Sprintf("lab-client-%s", client)
		if err := opts.DockerRunner.Build(clientDir, "Dockerfile", imageTag, nil); err != nil {
			log.Printf("client build failed: %s: %v", client, err)
			continue
		}
		successful++
	}
	if successful == 0 {
		return fmt.Errorf("no client images built successfully")
	}
	opts.Controller.SetClientVersions(opts.Clients)

	env := map[string]string{
		"LAB_SIMULATOR":    fmt.Sprintf("http://%s", addr),
		"LAB_TEST_PATTERN": opts.LimitPattern,
		"LAB_PARALLELISM":  fmt.Sprintf("%d", opts.Parallelism),
		"LAB_RANDOM_SEED":  fmt.Sprintf("%d", opts.RandomSeed),
		"LAB_LOGLEVEL":     fmt.Sprintf("%d", opts.LogLevel),
		"LAB_CLIENTS":      join(opts.Clients),
	}

	mounts := []string{}
	if opts.VectorsDir != "" {
		env["LAB_VECTOR_DIR"] = "/vectors"
		mounts = append(mounts, fmt.Sprintf("%s:/vectors:ro", opts.VectorsDir))
	}

	containerID, err := opts.DockerRunner.Run(docker.RunConfig{
		Image:   simImage,
		Env:     env,
		Mounts:  mounts,
		Network: "lab-net",
	})
	if err != nil {
		return err
	}

	// Stop simulator container when done
	defer func() {
		if logs, err := opts.DockerRunner.Logs(containerID); err == nil {
			if name, werr := writeSimLog(opts.Workspace, containerID, logs); werr == nil {
				opts.Controller.SetSimLog(name)
			}
		}
		_ = opts.DockerRunner.Remove(containerID)
		_ = opts.DockerRunner.RemoveNetwork("lab-net")
		_ = httpServer.Shutdown(context.Background())
	}()

	exitCode, err := opts.DockerRunner.Wait(containerID)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("simulator exited with code %d", exitCode)
	}
	return nil
}

func sanitize(s string) string {
	out := []rune{}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		case r >= '0' && r <= '9':
			out = append(out, r)
		case r == '-' || r == '_':
			out = append(out, r)
		case r == '/':
			out = append(out, '-')
		}
	}
	return string(out)
}

func join(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for i := 1; i < len(items); i++ {
		out += "," + items[i]
	}
	return out
}

func resolveBuildContext(simDir string) (string, string) {
	ctxFile := filepath.Join(simDir, "lab_context.txt")
	content, err := os.ReadFile(ctxFile)
	if err != nil {
		return simDir, filepath.Join(simDir, "Dockerfile")
	}
	rel := string(content)
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return simDir, filepath.Join(simDir, "Dockerfile")
	}
	ctxDir := filepath.Clean(filepath.Join(simDir, rel))
	return ctxDir, filepath.Join(simDir, "Dockerfile")
}

func writeSimLog(workspace, containerID, logs string) (string, error) {
	logDir := filepath.Join(workspace, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("simulator-%s.log", containerID)
	path := filepath.Join(logDir, name)
	return name, os.WriteFile(path, []byte(logs), 0o644)
}
