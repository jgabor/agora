//go:build mage

package main

import (
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

var Default = Build

func Build() error {
	return sh.RunV("go", "build", "-o", "build/agora", "./cmd/agora")
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

func Clean() error {
	return sh.Rm("agora")
}
