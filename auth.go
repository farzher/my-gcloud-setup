package main

import (
	"encoding/base64"
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

	// The persisted auth store is cheap to inspect and avoids a full Hermes
	// startup just to learn that the user is already logged in.
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
		"--command=exec sudo -n -i hermes auth add openai-codex --type oauth", "--", "-t")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	poll := time.NewTimer(3 * time.Second)
	defer poll.Stop()
	deadline := time.NewTimer(15 * time.Minute)
	defer deadline.Stop()

	finish := func() error {
		fmt.Fprintln(os.Stdout, "ChatGPT login confirmed.")
		return nil
	}

	for {
		select {
		case processErr := <-done:
			if loggedIn, _ := chatGPTAuthStatus(project, zone); loggedIn {
				return finish()
			}
			if processErr != nil {
				return processErr
			}
			return fmt.Errorf("ChatGPT login finished without saved credentials")
		case <-poll.C:
			if loggedIn, _ := chatGPTAuthStatus(project, zone); loggedIn {
				_ = cmd.Process.Kill()
				<-done
				return finish()
			}
			poll.Reset(3 * time.Second)
		case <-deadline.C:
			if loggedIn, _ := chatGPTAuthStatus(project, zone); loggedIn {
				_ = cmd.Process.Kill()
				<-done
				return finish()
			}
			_ = cmd.Process.Kill()
			<-done
			return fmt.Errorf("ChatGPT login timed out")
		}
	}
}

func chatGPTAuthStatus(project, zone string) (bool, error) {
	statusScript := strings.Replace(
		chatGPTAuthProbePython(),
		"raise SystemExit(0 if logged_in else 1)",
		`print("logged" if logged_in else "missing")`,
		1,
	)
	// gcloud SSH reparses --command through a remote shell. Encode the probe so
	// quotes/newlines in the Python source never participate in shell parsing.
	encoded := base64.StdEncoding.EncodeToString([]byte(statusScript))
	python := `import base64;exec(base64.b64decode("` + encoded + `"))`
	remote := "sudo -n python3 -c " + shellQuote(python)
	r, err := runTimeout(15*time.Second, "gcloud", "compute", "ssh", vmName,
		"--project="+project, "--zone="+zone, "--command="+remote, "--quiet")
	if err != nil {
		detail := strings.TrimSpace(usefulOutput(r))
		if detail != "" {
			return false, fmt.Errorf("ChatGPT auth probe: %w\n%s\ncommand: %s", err, detail, r.Command)
		}
		return false, fmt.Errorf("ChatGPT auth probe: %w\ncommand: %s", err, r.Command)
	}
	return strings.Contains(strings.ToLower(r.Stdout), "logged"), nil
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
