package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const (
	defaultImage   = "vxcontrol/kali-linux:latest" // same family used by PentAGI
	defaultTimeout = 60
	maxTimeout     = 10800
)

func main() {
	targetURL := flag.String("target", "", "Target URL to operate against (required for most uses). Injected as TARGET_URL env and available in commands.")
	imageName := flag.String("image", defaultImage, "Docker image to use for the terminal environment")
	cwd := flag.String("cwd", "/root", "Working directory inside the container")
	detach := flag.Bool("detach", false, "Run command in background (no output capture)")
	timeoutSec := flag.Int("timeout", defaultTimeout, "Command timeout in seconds (0 = server default, max 10800)")
	keepContainer := flag.Bool("keep", false, "Do not remove the container after the command finishes")
	pull := flag.Bool("pull", false, "Always pull the image before starting")
	envFile := flag.String("env", "", "Optional extra env vars file (KEY=VALUE per line)")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 || args[0] == "help" || args[0] == "-help" || args[0] == "--help" {
		usage()
		os.Exit(0)
	}

	funcName := args[0]
	if funcName != "terminal" {
		fmt.Fprintf(os.Stderr, "Only the 'terminal' function is supported in this standalone build.\n")
		fmt.Fprintf(os.Stderr, "Usage: url-tester terminal -input \"<command>\" [options]\n")
		os.Exit(1)
	}

	// Parse remaining args in the same style as original ftester
	input, message, err := parseTerminalArgs(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if input == "" {
		fmt.Fprintf(os.Stderr, "Error: -input is required\n")
		usage()
		os.Exit(1)
	}

	if *targetURL == "" {
		fmt.Fprintf(os.Stderr, "Warning: no -target URL supplied. TARGET_URL will be empty inside the container.\n")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Docker client: %v\n", err)
		os.Exit(1)
	}
	defer cli.Close()

	if *pull || !imageExists(ctx, cli, *imageName) {
		fmt.Printf("Pulling image %s ...\n", *imageName)
		if err := pullImage(ctx, cli, *imageName); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to pull image: %v\n", err)
			os.Exit(1)
		}
	}

	env := []string{
		"TARGET_URL=" + *targetURL,
		"DEBIAN_FRONTEND=noninteractive",
	}
	if *envFile != "" {
		extra, err := loadEnvFile(*envFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load env file: %v\n", err)
			os.Exit(1)
		}
		env = append(env, extra...)
	}

	timeout := time.Duration(*timeoutSec) * time.Second
	if *timeoutSec <= 0 || *timeoutSec > maxTimeout {
		timeout = time.Duration(defaultTimeout) * time.Second
	}

	fmt.Println("────────────────────────────────────────")
	fmt.Printf("url-tester – terminal\n")
	fmt.Printf("  target : %s\n", *targetURL)
	fmt.Printf("  image  : %s\n", *imageName)
	fmt.Printf("  cwd    : %s\n", *cwd)
	fmt.Printf("  detach : %v\n", *detach)
	fmt.Printf("  timeout: %s\n", timeout)
	if message != "" {
		fmt.Printf("  note   : %s\n", message)
	}
	fmt.Printf("  cmd    : %s\n", input)
	fmt.Println("────────────────────────────────────────")

	result, err := runInContainer(ctx, cli, *imageName, *cwd, input, env, *detach, timeout, !*keepContainer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Execution error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(result)
}

func usage() {
	fmt.Fprintf(os.Stderr, `url-tester – standalone terminal utility (extracted from PentAGI cmd/ftester)

Usage:
  url-tester -target <URL> terminal -input "<command>" [options]

Required for meaningful use:
  -target string
        Target URL (injected as $TARGET_URL inside the container)

Terminal arguments (same style as original ftester):
  -input string
        Command to execute inside the container (required)
  -message string
        Optional commentary / note (printed only)

Global options:
  -image string
        Docker image (default %q)
  -cwd string
        Working directory inside container (default "/root")
  -detach
        Run command in background (no stdout/stderr capture)
  -timeout int
        Timeout in seconds (default %d, max %d)
  -keep
        Keep the container after the command finishes
  -pull
        Always pull the image first
  -env string
        Path to extra KEY=VALUE env file

Examples:
  # Simple probe
  url-tester -target https://example.com terminal -input "curl -sI $TARGET_URL"

  # Nmap against the host part of the URL
  url-tester -target https://scanme.nmap.org terminal -input "nmap -sV $(echo $TARGET_URL | sed 's|https\?://||;s|/.*||')"

  # Interactive-style long runner (detach)
  url-tester -target https://example.com terminal -input "python3 -m http.server 8080" -detach -timeout 0

  # Keep container for later inspection
  url-tester -target https://example.com -keep terminal -input "id; uname -a"

`+"\n", defaultImage, defaultTimeout, maxTimeout)
}

func parseTerminalArgs(args []string) (input, message string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return "", "", fmt.Errorf("invalid argument format (expected -name): %s", a)
		}
		name := strings.TrimPrefix(a, "-")
		var value string
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			value = args[i+1]
			i++
		}
		switch name {
		case "input", "command": // accept both for compatibility
			input = value
		case "message":
			message = value
		case "cwd", "detach", "timeout": // already handled by global flags; ignore if repeated
		default:
			return "", "", fmt.Errorf("unknown terminal argument: -%s", name)
		}
	}
	return input, message, nil
}

func imageExists(ctx context.Context, cli *client.Client, ref string) bool {
	_, _, err := cli.ImageInspectWithRaw(ctx, ref)
	return err == nil
}

func pullImage(ctx context.Context, cli *client.Client, ref string) error {
	reader, err := cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, err = io.Copy(os.Stdout, reader)
	return err
}

func loadEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func runInContainer(
	ctx context.Context,
	cli *client.Client,
	imageName, cwd, cmd string,
	env []string,
	detach bool,
	timeout time.Duration,
	autoRemove bool,
) (string, error) {

	cfg := &container.Config{
		Image:      imageName,
		Cmd:        []string{"bash", "-lc", cmd},
		Env:        env,
		WorkingDir: cwd,
		Tty:        false,
		OpenStdin:  false,
	}

	hostCfg := &container.HostConfig{
		AutoRemove: autoRemove,
		// Network mode bridge so the container can reach the target URL
		NetworkMode: "bridge",
	}

	resp, err := cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	id := resp.ID

	if err := cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}

	if detach {
		return fmt.Sprintf("Started detached container %s (command running in background)", id[:12]), nil
	}

	// Wait with timeout
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	statusCh, errCh := cli.ContainerWait(waitCtx, id, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			// try to kill on timeout
			_ = cli.ContainerKill(context.Background(), id, "SIGKILL")
			return "", fmt.Errorf("wait: %w", err)
		}
	case st := <-statusCh:
		if st.StatusCode != 0 {
			// still collect logs
		}
	case <-waitCtx.Done():
		_ = cli.ContainerKill(context.Background(), id, "SIGKILL")
		return "", fmt.Errorf("command timed out after %s", timeout)
	}

	// Collect logs
	out, err := cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("logs: %w", err)
	}
	defer out.Close()

	var buf strings.Builder
	_, err = stdcopy.StdCopy(&buf, &buf, out)
	if err != nil && err != io.EOF {
		return buf.String(), fmt.Errorf("copy logs: %w", err)
	}
	return buf.String(), nil
}
