//go:build !windows

package core

import "os/exec"

func configureAgentCommand(cmd *exec.Cmd)              {}
func attachAgentProcess(cmd *exec.Cmd) (func(), error) { return func() {}, nil }
