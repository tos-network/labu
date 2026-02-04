package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tos-network/lab/internal/controller"
	"github.com/tos-network/lab/internal/docker"
	"github.com/tos-network/lab/internal/results"
	"github.com/tos-network/lab/internal/sim"
)

func main() {
	var (
		simName        = flag.String("sim", "", "simulator name (e.g. tos/rpc)")
		clientNames    = flag.String("client", "", "comma-separated client names")
		workspace      = flag.String("workspace", "./workspace", "workspace directory for logs/results")
		vectorsDir     = flag.String("vectors", "", "vectors directory to mount into simulator")
		simLimit       = flag.String("sim.limit", "", "regex to select suites/tests")
		simParallel    = flag.Int("sim.parallelism", 1, "test concurrency")
		simRandomSeed  = flag.Int64("sim.randomseed", 0, "random seed (0 means auto)")
		simLogLevel    = flag.Int("sim.loglevel", 2, "simulator log level (0-5)")
		simImage       = flag.String("sim.image", "", "override simulator image name")
		clientImageMap = flag.String("client.images", "", "override client images (name=image,name=image)")
	)
	flag.Parse()

	if *simName == "" {
		fmt.Fprintln(os.Stderr, "--sim is required")
		os.Exit(2)
	}
	if *clientNames == "" {
		fmt.Fprintln(os.Stderr, "--client is required")
		os.Exit(2)
	}

	ws, err := filepath.Abs(*workspace)
	if err != nil {
		log.Fatalf("workspace: %v", err)
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		log.Fatalf("workspace mkdir: %v", err)
	}

	clients := splitCSV(*clientNames)
	if len(clients) == 0 {
		log.Fatalf("no clients provided")
	}

	imageOverrides := parseImageOverrides(*clientImageMap)

	dockerRunner := docker.NewRunner(ws)
	ctrl := controller.New(ws, dockerRunner)
	resWriter := results.NewWriter(ws)

	opts := sim.Options{
		Simulator:       *simName,
		Clients:         clients,
		SimulatorImage:  *simImage,
		ClientImages:    imageOverrides,
		VectorsDir:      *vectorsDir,
		LimitPattern:    *simLimit,
		Parallelism:     *simParallel,
		RandomSeed:      *simRandomSeed,
		LogLevel:        *simLogLevel,
		Workspace:       ws,
		Controller:      ctrl,
		ResultWriter:    resWriter,
		DockerRunner:    dockerRunner,
	}

	if err := sim.Run(opts); err != nil {
		log.Fatalf("simulation failed: %v", err)
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseImageOverrides(s string) map[string]string {
	out := make(map[string]string)
	if s == "" {
		return out
	}
	pairs := strings.Split(s, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return out
}
