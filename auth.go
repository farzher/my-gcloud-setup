package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	qrterminal "github.com/mdp/qrterminal/v3"
)

var googleURLPattern = regexp.MustCompile(`https://accounts\.google\.com/[^\s]+`)

func googleBrowserAuthCmd() tea.Cmd {
	cmd := exec.Command("gcloud", "auth", "login")
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return authDoneMsg{err} })
}
func googleQRAuthCmd() tea.Cmd {
	cmd := exec.Command(os.Args[0], "__google-qr")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return authDoneMsg{err} })
}
func switchGoogleAccountCmd(account string) tea.Cmd {
	return func() tea.Msg {
		_, err := runTimeout(20*time.Second, "gcloud", "config", "set", "account", account, "--quiet")
		return authDoneMsg{err}
	}
}

func chatGPTAuthCmd(cfg config) tea.Cmd {
	cmd := exec.Command(os.Args[0], "__chatgpt-auth", cfg.Project)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return authDoneMsg{err} })
}

func githubAuthCmd() tea.Cmd {
	gh := ghPath()
	if gh == "" {
		return func() tea.Msg { return authDoneMsg{fmt.Errorf("GitHub CLI not found; run run.bat again")} }
	}
	cmd := exec.Command(gh, "auth", "login", "--hostname", "github.com", "--git-protocol", "ssh", "--web")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return authDoneMsg{err} })
}

func remoteSSHCmd(cfg config) tea.Cmd {
	cmd := exec.Command("gcloud", "compute", "ssh", vmName, "--project="+cfg.Project, "--zone="+zone)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return externalDoneMsg{err} })
}
func remoteHermesCmd(cfg config) tea.Cmd {
	remote := `exec sudo -n -i bash -lc 'cd /website 2>/dev/null || cd /root; exec hermes'`
	cmd := exec.Command("gcloud", "compute", "ssh", vmName, "--project="+cfg.Project, "--zone="+zone, "--command="+remote, "--", "-t")
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return externalDoneMsg{err} })
}
func remoteGatewayCmd(cfg config) tea.Cmd {
	remote := `exec sudo -n -i hermes gateway setup`
	cmd := exec.Command("gcloud", "compute", "ssh", vmName, "--project="+cfg.Project, "--zone="+zone, "--command="+remote, "--", "-t")
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return externalDoneMsg{err} })
}

func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg { return browserDoneMsg{openBrowser(url)} }
}
func openBrowser(url string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("open this URL: %s", url)
	}
	return exec.Command("cmd", "/c", "start", "", url).Start()
}

func runGoogleQRAuth() error {
	cmd := exec.Command("gcloud", "auth", "login", "--no-launch-browser")
	writer := &googleAuthWriter{out: os.Stdout}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = writer, writer, os.Stdin
	return cmd.Run()
}

type googleAuthWriter struct {
	out   io.Writer
	buf   string
	shown bool
}

func (w *googleAuthWriter) Write(p []byte) (int, error) {
	if _, err := w.out.Write(p); err != nil {
		return 0, err
	}
	if w.shown {
		return len(p), nil
	}
	w.buf += string(p)
	if url := googleURLPattern.FindString(w.buf); url != "" {
		w.shown = true
		renderQR(w.out, url)
	}
	if len(w.buf) > 64*1024 {
		w.buf = w.buf[len(w.buf)-4096:]
	}
	return len(p), nil
}

func runChatGPTAuth(project string) error {
	const url = "https://auth.openai.com/codex/device"
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "ChatGPT")
	fmt.Fprintln(os.Stdout)
	renderQR(os.Stdout, url)
	fmt.Fprintln(os.Stdout, url)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Use the code shown below.")
	fmt.Fprintln(os.Stdout)
	cmd := exec.Command("gcloud", "compute", "ssh", vmName,
		"--project="+project, "--zone="+zone,
		"--command=exec sudo -n -i hermes auth add openai-codex", "--", "-t")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func renderQR(out io.Writer, url string) {
	fmt.Fprintln(out)
	qrterminal.GenerateWithConfig(url, qrterminal.Config{Level: qrterminal.M, Writer: out, HalfBlocks: true, QuietZone: 1})
	fmt.Fprintln(out)
}

func ghPath() string {
	if p, err := exec.LookPath("gh"); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			filepath.Join(os.Getenv("ProgramFiles"), "GitHub CLI", "gh.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "GitHub CLI", "gh.exe"),
		} {
			if strings.TrimSpace(p) != "" {
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
	}
	return ""
}
