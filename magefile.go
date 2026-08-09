//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	binName = "agora"
	pkgPath = "./cmd/agora"
)

func binPath() string {
	name := binName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join("build", name)
}

func installDir() string {
	if dir := os.Getenv("GOBIN"); dir != "" {
		return dir
	}
	if dir, err := sh.Output("go", "env", "GOBIN"); err == nil && dir != "" {
		return dir
	}
	gopath, err := sh.Output("go", "env", "GOPATH")
	if err != nil || gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}
	return filepath.Join(gopath, "bin")
}

var Default = Build

// Build compiles agora into build/agora with size-optimized flags.
func Build() error {
	if err := os.MkdirAll("build", 0o755); err != nil {
		return err
	}
	out := binPath()
	if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
		return err
	}
	return sh.RunWith(map[string]string{"CGO_ENABLED": "0"},
		"go", "build",
		"-trimpath",
		"-ldflags=-s -w",
		"-o", out,
		pkgPath,
	)
}

// Install builds and installs agora into GOBIN.
func Install() error {
	mg.Deps(Build)
	dest := installDir()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	target := filepath.Join(dest, filepath.Base(binPath()))
	if err := sh.Copy(target, binPath()); err != nil {
		return err
	}
	fmt.Printf("installed %s\n", target)
	return nil
}

func Test() error {
	return sh.RunV("go", "test", "-race", "-coverprofile=coverage.out", "./...")
}

func Lint() error {
	return sh.RunV("golangci-lint", "run", "./...")
}

func Vet() error {
	return sh.RunV("go", "vet", "./...")
}

type Check mg.Namespace

func (Check) All() error {
	mg.Deps(Lint, Vet, Test)
	return nil
}

type Eval mg.Namespace

const evaluatorScript = "./scripts/eval-cli-discovery.sh"

// runEvaluator preserves typed target values as literal argv and bounds target errors.
func runEvaluator(args ...string) error {
	cmd := exec.Command(evaluatorScript, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if code := exitErr.ExitCode(); code > 0 {
				return mg.Fatalf(code, "evaluator failed with exit code %d", code)
			}
			return mg.Fatalf(1, "evaluator terminated without an exit code")
		}
		return fmt.Errorf("evaluator failed to start: %w", err)
	}
	return nil
}

// CliDiscovery runs an opt-in live evaluator trial.
func (Eval) CliDiscovery(output string, model, authFile, timeout *string, quiet *bool) error {
	if output == "" {
		return fmt.Errorf("output is required")
	}
	args := []string{"--output", output}
	if model != nil {
		args = append(args, "--model", *model)
	}
	if authFile != nil {
		args = append(args, "--auth-file", *authFile)
	}
	if timeout != nil {
		args = append(args, "--timeout", *timeout)
	}
	if quiet != nil && *quiet {
		args = append(args, "--quiet")
	}
	return runEvaluator(args...)
}

// CliDiscoveryHelp prints the evaluator's authoritative usage.
func (Eval) CliDiscoveryHelp() error {
	return runEvaluator("--help")
}

// CliDiscoverySelfTest runs the full provider-free evaluator self-test.
func (Eval) CliDiscoverySelfTest() error {
	return runEvaluator("--self-test")
}

// CliDiscoveryOfflineSelfTest runs the provider-free offline evaluator self-test.
func (Eval) CliDiscoveryOfflineSelfTest() error {
	return runEvaluator("--analysis-self-test")
}

// E2E runs the optional termctrl-based terminal smoke test.
func E2E() error {
	return sh.RunV("./scripts/e2e-termctrl.sh")
}

// Clean removes build artifacts.
func Clean() error {
	return os.RemoveAll("build")
}
