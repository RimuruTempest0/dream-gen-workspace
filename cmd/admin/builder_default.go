//go:build !desktopqa

package main

import "imageworkbench/internal/admin"

func builder() admin.Builder { return admin.ScriptBuilder{} }

func releaseHost() admin.ReleaseHost { return admin.GitHubHost{} }
