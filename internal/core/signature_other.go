//go:build !windows

package core

func authenticodeValid(string) bool { return false }
