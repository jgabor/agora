//go:build mage

package main

import (
	"fmt"
	"os"
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

// E2E runs the optional rmux-based terminal smoke test in scripts/e2e-rmux.sh.
func E2E() error {
	return sh.RunV("./scripts/e2e-rmux.sh")
}

// Clean removes build artifacts.
func Clean() error {
	return os.RemoveAll("build")
}


