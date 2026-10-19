//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(`{"event":"error","code":"unsupported","message":"浏览器读取仅支持 Windows"}`)
	os.Exit(1)
}
