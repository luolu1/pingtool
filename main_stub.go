//go:build !windows

// The shipped tool is a native Windows GUI application built on
// github.com/lxn/walk. This stub keeps `go build ./...` usable on other
// platforms (for example to run the pinger unit tests).
package main

import "fmt"

func main() {
	fmt.Println("PingTool 是 Windows 原生 GUI 程序，请在 Windows 上构建/运行：")
	fmt.Println(`  go build -ldflags "-H windowsgui -s -w" -o PingTool.exe .`)
}
