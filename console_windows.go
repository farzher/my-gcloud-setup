//go:build windows

package main

import (
	"os"
	"syscall"
)

const consoleUTF8 = 65001

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	setConsoleCP            = kernel32.NewProc("SetConsoleCP")
	setConsoleOutputCP      = kernel32.NewProc("SetConsoleOutputCP")
	flushConsoleInputBuffer = kernel32.NewProc("FlushConsoleInputBuffer")
)

func configureConsole() {
	_, _, _ = setConsoleCP.Call(consoleUTF8)
	_, _, _ = setConsoleOutputCP.Call(consoleUTF8)
}

func flushConsoleInput() {
	_, _, _ = flushConsoleInputBuffer.Call(os.Stdin.Fd())
}
