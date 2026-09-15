package main

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"os"
)

func main() {
	configureConsole()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "__google-qr":
			if err := runGoogleQRAuth(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "__billing-qr":
			if len(os.Args) < 3 {
				os.Exit(2)
			}
			if err := runBillingQR(os.Args[2]); err != nil {
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

	for {
		final, err := tea.NewProgram(initialModel()).Run()
		if err != nil {
			fmt.Fprintln(os.Stderr, "cloud:", err)
			os.Exit(1)
		}
		m, ok := final.(model)
		if !ok || m.external == externalNone {
			return
		}
		if err = runExternalSession(m.external, m.cfg); err != nil {
			fmt.Fprintln(os.Stderr, "cloud:", err)
			fmt.Fprintln(os.Stderr, "Press Enter to return to cloud.")
			_, _ = fmt.Fscanln(os.Stdin)
		}
		configureConsole()
	}
}
