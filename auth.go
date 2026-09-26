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

func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg { return browserDoneMsg{openBrowser(url)} }
}
func openBrowser(url string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("open this URL: %s", url)
	}
	return exec.Command("cmd", "/c", "start", "", url).Start()
}

func cloudConsoleURL(project string) string {
	return "https://console.cloud.google.com/home/dashboard?project=" + project
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
	zoneResult, err := runTimeout(30*time.Second, "gcloud", "compute", "instances", "list", "--project="+project, "--filter=name="+vmName, "--format=value(zone.basename())")
	if err != nil {
		return fmt.Errorf("find VM zone: %w", err)
	}
	zone := firstLine(zoneResult.Stdout)
	if zone == "" {
		return fmt.Errorf("VM zone not found")
	}

	// Never make the user authorize again when the remote Hermes credential
	// store already has a working Codex login.
	if loggedIn, _ := chatGPTAuthStatus(project, zone); loggedIn {
		fmt.Fprintln(os.Stdout, "ChatGPT already logged in.")
		return nil
	}

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
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	poll := time.NewTimer(5 * time.Second)
	defer poll.Stop()
	deadline := time.NewTimer(15 * time.Minute)
	defer deadline.Stop()

	for {
		select {
		case err := <-done:
			if err == nil {
				return nil
			}
			// The SSH/device-code process can fail to exit after credentials were
			// already committed. Treat verified remote auth as success.
			if loggedIn, _ := chatGPTAuthStatus(project, zone); loggedIn {
				return nil
			}
			return err
		case <-poll.C:
			if loggedIn, _ := chatGPTAuthStatus(project, zone); loggedIn {
				_ = cmd.Process.Kill()
				<-done
				fmt.Fprintln(os.Stdout, "ChatGPT login confirmed.")
				return nil
			}
			poll.Reset(5 * time.Second)
		case <-deadline.C:
			if loggedIn, _ := chatGPTAuthStatus(project, zone); loggedIn {
				_ = cmd.Process.Kill()
				<-done
				return nil
			}
			_ = cmd.Process.Kill()
			<-done
			return fmt.Errorf("ChatGPT login timed out")
		}
	}
}

func chatGPTAuthStatus(project, zone string) (bool, error) {
	r, err := runTimeout(30*time.Second, "gcloud", "compute", "ssh", vmName,
		"--project="+project, "--zone="+zone,
		"--command=sudo -n -i hermes auth status openai-codex", "--quiet")
	if err != nil {
		return false, err
	}
	return chatGPTLoggedIn(usefulOutput(r)), nil
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
