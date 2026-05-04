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

// Build compiles agora into ./bin with size-optimized flags, then compresses.
func Build() error {
	if err := os.MkdirAll("build", 0o755); err != nil {
		return err
	}
	out := binPath()
	// Remove any prior binary so go build always writes a fresh one.
	// Go's linker skips the write when the target already contains a
	// matching Go BuildID — which a UPX-packed binary still carries,
	// so subsequent builds would leave the packed file untouched and
	// UPX would then fail with AlreadyPackedException.
	if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := sh.RunWith(map[string]string{"CGO_ENABLED": "0"},
		"go", "build",
		"-trimpath",
		"-ldflags=-s -w",
		"-o", out,
		pkgPath,
	); err != nil {
		return err
	}
	return compress(out)
}

// Install builds, compresses, and installs agora into GOBIN.
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

// Clean removes build artifacts.
func Clean() error {
	return os.RemoveAll("build")
}

// compress runs UPX on the built binary. By default uses --no-lzma (fast
// decompression). Set AGORA_LZMA=1 to opt into --lzma for smaller binary
// at the cost of slower startup.
func compress(path string) error {
	upx, err := exec.LookPath("upx")
	if err != nil {
		fmt.Println("upx not found; skipping compression")
		return nil
	}
	args := []string{"--best", "--overlay=strip", "--no-lzma", path}
	if os.Getenv("AGORA_LZMA") == "1" {
		args = []string{"--best", "--overlay=strip", "--lzma", path}
	}
	fmt.Printf("compressing %s with upx\n", path)
	return sh.Run(upx, args...)
}
