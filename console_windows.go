//go:build windows

package main

import "syscall"

const consoleUTF8 = 65001

func configureConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleCP := kernel32.NewProc("SetConsoleCP")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	_, _, _ = setConsoleCP.Call(consoleUTF8)
	_, _, _ = setConsoleOutputCP.Call(consoleUTF8)
}
