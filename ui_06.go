package main

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "__google-qr":
			if err := runGoogleQRAuth(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "__chatgpt-auth":
			if len(os.Args) < 3 {
				os.Exit(2)
			}
			if err := runChatGPTAuth(os.Args[2]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}
	m := initialModel()
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cloud:", err)
		os.Exit(1)
	}
}
