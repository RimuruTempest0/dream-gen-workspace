//go:build !windows

package main

import (
	"fmt"

	"imageworkbench/internal/i18n"
)

func fatal(message string)                 { fmt.Println(message) }
func prepareRuntime(path string) error     { return nil }
func claimInstance() (func(), bool, error) { return func() {}, true, nil }
func openDirectory(path string) error      { return i18n.Fail("app.openData.windowsOnly") }
