package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	qrterminal "github.com/mdp/qrterminal/v3"
)

var authURLPattern = regexp.MustCompile(`https://accounts\.google\.com/[^\s]+`)

type authWriter struct {
	out   io.Writer
	buf   string
	shown bool
}

func (w *authWriter) Write(p []byte) (int, error) {
	if _, err := w.out.Write(p); err != nil {
		return 0, err
	}
	if w.shown {
		return len(p), nil
	}

	w.buf += string(p)
	if url := authURLPattern.FindString(w.buf); url != "" {
		w.shown = true
		renderQR(w.out, url)
	}

	// Avoid retaining unlimited gcloud output before the URL appears.
	if len(w.buf) > 64*1024 {
		w.buf = w.buf[len(w.buf)-4096:]
	}
	return len(p), nil
}

func renderQR(out io.Writer, url string) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, strings.Repeat("=", 60))
	fmt.Fprintln(out, "Scan this QR code on the other device")
	fmt.Fprintln(out)

	qrterminal.GenerateWithConfig(url, qrterminal.Config{
		Level:      qrterminal.M,
		Writer:     out,
		HalfBlocks: true,
		QuietZone:  1,
	})

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Direct link:")
	fmt.Fprintln(out, url)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "After approval, paste the verification code below.")
	fmt.Fprintln(out, strings.Repeat("=", 60))
	fmt.Fprintln(out)
}

func main() {
	cmd := exec.Command("gcloud", "auth", "login", "--no-launch-browser")
	writer := &authWriter{out: os.Stdout}
	cmd.Stdout = writer
	cmd.Stderr = writer
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "remote login failed:", err)
		os.Exit(1)
	}
}
