package main

import (
	"os"
	"os/exec"
)

var (
	changeWorkingDir = os.Chdir
	lookPath         = exec.LookPath
	runCommandOutput = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	}
)
