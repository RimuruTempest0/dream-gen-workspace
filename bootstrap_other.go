//go:build !windows

package main

import "fmt"

func fatal(message string)                 { fmt.Println(message) }
func prepareRuntime(path string) error     { return nil }
func claimInstance() (func(), bool, error) { return func() {}, true, nil }
func openDirectory(path string) error      { return fmt.Errorf("打开数据目录仅支持 Windows") }
