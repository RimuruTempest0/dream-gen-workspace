//go:build !windows

package core

import "runtime"

func osVersion() string { return runtime.GOOS }
