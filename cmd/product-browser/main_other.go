//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(`{"event":"error","code":"unsupported","message":"reading in a browser works only on Windows"}`)
	os.Exit(1)
}
