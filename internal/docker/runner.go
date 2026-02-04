package docker

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type Runner struct {
	workspace string
}

type RunConfig struct {
	Image      string
	Name       string
	Env        map[string]string
	Mounts     []string
	Network    string
	Workdir    string
	Entrypoint []string
	Args       []string
}

func NewRunner(workspace string) *Runner {
	return &Runner{workspace: workspace}
}

func (r *Runner) Build(ctxDir, dockerfile, tag string, buildArgs map[string]string) error {
	args := []string{"build", "-t", tag}
	if dockerfile != "" {
		args = append(args, "-f", dockerfile)
	}
	for k, v := range buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, ctxDir)
	_, _, err := r.run("docker", args...)
	return err
}

func (r *Runner) Run(cfg RunConfig) (string, error) {
	args := []string{"run", "-d"}
	if cfg.Name != "" {
		args = append(args, "--name", cfg.Name)
	}
	if cfg.Workdir != "" {
		args = append(args, "-w", cfg.Workdir)
	}
	if cfg.Network != "" {
		args = append(args, "--network", cfg.Network)
	}
	for _, m := range cfg.Mounts {
		args = append(args, "-v", m)
	}
	for k, v := range cfg.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	if len(cfg.Entrypoint) > 0 {
		args = append(args, "--entrypoint", strings.Join(cfg.Entrypoint, " "))
	}
	args = append(args, cfg.Image)
	args = append(args, cfg.Args...)

	stdout, _, err := r.run("docker", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}

func (r *Runner) Exec(containerID string, cmd []string) (int, string, string, error) {
	args := append([]string{"exec", containerID}, cmd...)
	stdout, stderr, err := r.run("docker", args...)
	if err != nil {
		return exitCode(err), stdout, stderr, err
	}
	return 0, stdout, stderr, nil
}

func (r *Runner) Stop(containerID string) error {
	_, _, err := r.run("docker", "stop", containerID)
	return err
}

func (r *Runner) Remove(containerID string) error {
	_, _, err := r.run("docker", "rm", "-f", containerID)
	return err
}

func (r *Runner) Wait(containerID string) (int, error) {
	stdout, _, err := r.run("docker", "wait", containerID)
	if err != nil {
		return exitCode(err), err
	}
	codeStr := strings.TrimSpace(stdout)
	if codeStr == "" {
		return 0, nil
	}
	var code int
	_, scanErr := fmt.Sscanf(codeStr, "%d", &code)
	if scanErr != nil {
		return 0, scanErr
	}
	return code, nil
}

func (r *Runner) Logs(containerID string) (string, error) {
	stdout, _, err := r.run("docker", "logs", containerID)
	return stdout, err
}

func (r *Runner) CreateNetwork(name string) error {
	_, _, err := r.run("docker", "network", "create", name)
	return err
}

func (r *Runner) RemoveNetwork(name string) error {
	_, _, err := r.run("docker", "network", "rm", name)
	return err
}

func (r *Runner) ConnectNetwork(name, containerID string) error {
	_, _, err := r.run("docker", "network", "connect", name, containerID)
	return err
}

func (r *Runner) DisconnectNetwork(name, containerID string) error {
	_, _, err := r.run("docker", "network", "disconnect", name, containerID)
	return err
}

func (r *Runner) InspectIP(network, containerID string) (string, error) {
	format := fmt.Sprintf("{{.NetworkSettings.Networks.%s.IPAddress}}", network)
	stdout, _, err := r.run("docker", "inspect", "-f", format, containerID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}

func (r *Runner) run(name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}
