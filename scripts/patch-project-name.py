from pathlib import Path

p = Path("main.go")
s = p.read_text()

# Persistent human-readable project names, keyed by Google account.
old = '''type config struct {
\tProjects map[string]string `json:"projects,omitempty"`

\t// Runtime-only fields. Each Google account gets one remembered managed project.
\tAccount string `json:"-"`
\tProject string `json:"-"`
}
'''
new = '''type config struct {
\tProjects map[string]string `json:"projects,omitempty"`
\tNames    map[string]string `json:"names,omitempty"`

\t// Runtime-only fields. Each Google account gets one remembered managed project.
\tAccount string `json:"-"`
\tProject string `json:"-"`
}
'''
assert old in s
s = s.replace(old, new, 1)

old = '''func (c *config) setProject(account, project string) {
\tif c.Projects == nil {
\t\tc.Projects = map[string]string{}
\t}
\tc.Projects[account] = project
\tc.Account = account
\tc.Project = project
}
'''
new = old + '''
func (c config) nameFor(account string) string {
\tif c.Names == nil {
\t\treturn ""
\t}
\treturn c.Names[account]
}

func (c *config) setName(account, name string) {
\tif c.Names == nil {
\t\tc.Names = map[string]string{}
\t}
\tc.Names[account] = name
}
'''
assert old in s
s = s.replace(old, new, 1)

# Detect/display the real current Google Cloud project name.
old = '''type cloudState struct {
\tGcloud      bool
\tAccount     string
\tBilling     []billingAccount
\tProjectOK   bool
\tVMExists    bool
\tInstance    instanceInfo
\tStaticIP    string
\tHermesReady bool
}
'''
new = '''type cloudState struct {
\tGcloud      bool
\tAccount     string
\tBilling     []billingAccount
\tProjectOK   bool
\tProjectName string
\tVMExists    bool
\tInstance    instanceInfo
\tStaticIP    string
\tHermesReady bool
}
'''
assert old in s
s = s.replace(old, new, 1)

# Minimal inline name editor state.
old = '''\tvmScanBusy    bool
\tvmWarningAck  bool

\tmenu    []string
'''
new = '''\tvmScanBusy    bool
\tvmWarningAck  bool

\tprojectNameInput string
\tnameEditing      bool
\tnameError        string

\tmenu    []string
'''
assert old in s
s = s.replace(old, new, 1)

old = '''type browserOpenedMsg struct{ err error }
type piFinishedMsg struct{ err error }
'''
new = '''type browserOpenedMsg struct{ err error }
type piFinishedMsg struct{ err error }
type renameFinishedMsg struct {
\tcfg    config
\toutput string
\terr    error
}
'''
assert old in s
s = s.replace(old, new, 1)

# Reset the editor when switching accounts.
old = '''\t\t\tm.vmScanBusy = false
\t\t\tm.vmWarningAck = false
\t\t}
'''
new = '''\t\t\tm.vmScanBusy = false
\t\t\tm.vmWarningAck = false
\t\t\tm.projectNameInput = ""
\t\t\tm.nameEditing = false
\t\t\tm.nameError = ""
\t\t}
'''
assert old in s
s = s.replace(old, new, 1)

# Handle successful/failed rename, then redetect the project.
marker = '''\tcase authFinishedMsg:
'''
addition = '''\tcase renameFinishedMsg:
\t\tm.busy = false
\t\tm.lastOutput = msg.output
\t\tif msg.err != nil {
\t\t\tm.lastErr = msg.err
\t\t\tm.returnScreen = screenDashboard
\t\t\tm.screen = screenDetails
\t\t\treturn m, nil
\t\t}
\t\tm.cfg = msg.cfg
\t\tm.nameEditing = false
\t\tm.nameError = ""
\t\tm.projectNameInput = ""
\t\tm.busy = true
\t\treturn m, detectCmd(m.cfg)

'''
assert marker in s
s = s.replace(marker, addition + marker, 1)

# Inline name editing gets first shot at printable keys, including q/a/n.
marker = '''\tswitch m.screen {
'''
handler = '''\tif m.nameEditing {
\t\tswitch k {
\t\tcase "enter":
\t\t\tname, err := cleanProjectName(m.projectNameInput)
\t\t\tif err != nil {
\t\t\t\tm.nameError = err.Error()
\t\t\t\treturn m, nil
\t\t\t}
\t\t\tm.projectNameInput = name
\t\t\tm.nameError = ""
\t\t\tif m.cfg.Project == "" {
\t\t\t\tm.cfg.setName(m.state.Account, name)
\t\t\t\tif err := saveConfig(m.cfg); err != nil {
\t\t\t\t\tm.lastErr = err
\t\t\t\t\tm.lastOutput = err.Error()
\t\t\t\t\tm.returnScreen = screenCreate
\t\t\t\t\tm.screen = screenDetails
\t\t\t\t\treturn m, nil
\t\t\t\t}
\t\t\t\tm.nameEditing = false
\t\t\t\treturn m, m.routeAfterDetect()
\t\t\t}
\t\t\tm.busy = true
\t\t\treturn m, renameProjectCmd(m.cfg, name)
\t\tcase "esc":
\t\t\tif m.cfg.Project != "" {
\t\t\t\tm.nameEditing = false
\t\t\t\tm.nameError = ""
\t\t\t\tm.projectNameInput = ""
\t\t\t}
\t\t\treturn m, nil
\t\tcase "backspace":
\t\t\trunes := []rune(m.projectNameInput)
\t\t\tif len(runes) > 0 {
\t\t\t\tm.projectNameInput = string(runes[:len(runes)-1])
\t\t\t}
\t\t\tm.nameError = ""
\t\t\treturn m, nil
\t\tdefault:
\t\t\ttext := key.Key().Text
\t\t\tif text != "" && len([]rune(m.projectNameInput+text)) <= 60 {
\t\t\t\tm.projectNameInput += text
\t\t\t\tm.nameError = ""
\t\t\t}
\t\t\treturn m, nil
\t\t}
\t}

'''
assert marker in s
s = s.replace(marker, handler + marker, 1)

# Rename shortcut on the finished Server screen.
old = '''\t\tcase "r":
\t\t\tm.busy = true
\t\t\tm.statusText = "Refreshing"
\t\t\treturn m, detectCmd(m.cfg)
\t\tcase "q":
'''
new = '''\t\tcase "r":
\t\t\tm.busy = true
\t\t\tm.statusText = "Refreshing"
\t\t\treturn m, detectCmd(m.cfg)
\t\tcase "n":
\t\t\tm.nameEditing = true
\t\t\tm.nameError = ""
\t\t\tm.projectNameInput = m.state.ProjectName
\t\t\treturn m, nil
\t\tcase "q":
'''
assert old in s
s = s.replace(old, new, 1)

# Require a name before the account-wide VM warning / automatic provisioning.
old = '''\tif len(m.state.Billing) == 1 {
\t\tm.billingID = m.state.Billing[0].id()
\t} else if m.billingID == "" {
\t\tm.screen = screenBillingPick
\t\treturn nil
\t}

\t// Before creating anything, finish the account-wide VM scan. Existing VMs
'''
new = '''\tif len(m.state.Billing) == 1 {
\t\tm.billingID = m.state.Billing[0].id()
\t} else if m.billingID == "" {
\t\tm.screen = screenBillingPick
\t\treturn nil
\t}

\tif m.cfg.Project == "" && strings.TrimSpace(m.cfg.nameFor(m.state.Account)) == "" {
\t\tm.screen = screenCreate
\t\tm.nameEditing = true
\t\tm.nameError = ""
\t\treturn nil
\t}

\t// Before creating anything, finish the account-wide VM scan. Existing VMs
'''
assert old in s
s = s.replace(old, new, 1)

# New projects use the chosen display name rather than a generic name.
old = '''\tfor attempt := 0; attempt < 5; attempt++ {
\t\tid := "cloud-" + randomHex(5)
\t\tctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
\t\tres, err := run(ctx, "gcloud", "projects", "create", id,
\t\t\t"--name=Cloud server", "--labels=cloud-charm=managed", "--quiet")
'''
new = '''\tdisplayName := strings.TrimSpace(cfg.nameFor(cfg.Account))
\tif displayName == "" {
\t\tdisplayName = "Cloud server"
\t}
\tfor attempt := 0; attempt < 5; attempt++ {
\t\tid := "cloud-" + randomHex(5)
\t\tctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
\t\tres, err := run(ctx, "gcloud", "projects", "create", id,
\t\t\t"--name="+displayName, "--labels=cloud-charm=managed", "--quiet")
'''
assert old in s
s = s.replace(old, new, 1)

# Read the display name from GCP so external renames are reflected too.
old = '''\ts.ProjectOK = strings.TrimSpace(res.Stdout) != ""

\towner, err := projectOwner(ctx, project, adminEmail)
'''
new = '''\ts.ProjectOK = strings.TrimSpace(res.Stdout) != ""

\tnameRes, nameErr := run(ctx, "gcloud", "projects", "describe", project, "--format=value(name)")
\tif nameErr == nil {
\t\ts.ProjectName = firstLine(nameRes.Stdout)
\t}

\towner, err := projectOwner(ctx, project, adminEmail)
'''
assert old in s
s = s.replace(old, new, 1)

# Rename command.
marker = '''func lifecycleCmd(name string, cfg config, action string) tea.Cmd {
'''
rename = '''func renameProjectCmd(cfg config, name string) tea.Cmd {
\treturn func() tea.Msg {
\t\tctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
\t\tdefer cancel()
\t\tres, err := run(ctx, "gcloud", "projects", "update", cfg.Project, "--name="+name, "--quiet")
\t\tif err == nil {
\t\t\tcfg.setName(cfg.Account, name)
\t\t\tif saveErr := saveConfig(cfg); saveErr != nil {
\t\t\t\terr = saveErr
\t\t\t}
\t\t}
\t\treturn renameFinishedMsg{cfg: cfg, output: usefulOutput(res), err: err}
\t}
}

'''
assert marker in s
s = s.replace(marker, rename + marker, 1)

# Project-name validation / domain-friendly normalization.
marker = '''func randomHex(n int) string {
'''
clean = '''func cleanProjectName(input string) (string, error) {
\tinput = strings.TrimSpace(input)
\tvar b strings.Builder
\tlastDash := false
\tfor _, r := range input {
\t\tallowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
\t\t\t(r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '\\'' || r == '"' || r == '!'
\t\tif r == '.' || r == '_' || r == '/' || r == '\\\\' || r == ':' {
\t\t\tr = '-'
\t\t\tallowed = true
\t\t}
\t\tif !allowed {
\t\t\tcontinue
\t\t}
\t\tif r == '-' {
\t\t\tif lastDash {
\t\t\t\tcontinue
\t\t\t}
\t\t\tlastDash = true
\t\t} else {
\t\t\tlastDash = false
\t\t}
\t\tb.WriteRune(r)
\t}
\tname := strings.TrimSpace(b.String())
\trunes := []rune(name)
\tif len(runes) > 30 {
\t\trunes = runes[:30]
\t\tname = strings.TrimSpace(string(runes))
\t}
\tname = strings.TrimRight(name, " -'\\\"!")
\tif len([]rune(name)) < 4 {
\t\treturn "", errors.New("4–30 chars")
\t}
\tlast := []rune(name)[len([]rune(name))-1]
\tif !((last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') || (last >= '0' && last <= '9')) {
\t\treturn "", errors.New("end with letter/number")
\t}
\treturn name, nil
}

'''
assert marker in s
s = s.replace(marker, clean + marker, 1)

# Initialize name storage when loading old configs.
old = '''func loadConfig() config {
\tcfg := config{Projects: map[string]string{}}
'''
new = '''func loadConfig() config {
\tcfg := config{Projects: map[string]string{}, Names: map[string]string{}}
'''
assert old in s
s = s.replace(old, new, 1)

old = '''\tif cfg.Projects == nil {
\t\tcfg.Projects = map[string]string{}
\t}
\treturn cfg
}
'''
new = '''\tif cfg.Projects == nil {
\t\tcfg.Projects = map[string]string{}
\t}
\tif cfg.Names == nil {
\t\tcfg.Names = map[string]string{}
\t}
\treturn cfg
}
'''
assert old in s
s = s.replace(old, new, 1)

# Server screen: same screen becomes the name entry/rename editor.
old = '''func (m model) renderServer() string {
\tvar b strings.Builder
\tb.WriteString(m.header())
\tb.WriteString("\\n\\n")
\tb.WriteString(titleStyle.Render("Server"))
\tif m.state.Account != "" {
\t\tb.WriteString("  " + mutedStyle.Render(m.state.Account))
\t}
\tb.WriteString("\\n\\n")
'''
new = '''func (m model) renderServer() string {
\tvar b strings.Builder
\tb.WriteString(m.header())
\tb.WriteString("\\n\\n")
\tb.WriteString(titleStyle.Render("Server"))
\tprojectName := m.state.ProjectName
\tif projectName == "" {
\t\tprojectName = m.cfg.nameFor(m.state.Account)
\t}
\tif projectName != "" && !m.nameEditing {
\t\tb.WriteString("  " + mutedStyle.Render(projectName))
\t}
\tif m.state.Account != "" {
\t\tb.WriteString("\\n" + mutedStyle.Render(m.state.Account))
\t}
\tb.WriteString("\\n\\n")

\tif m.nameEditing {
\t\tlabel := "Project name"
\t\tif m.cfg.Project != "" {
\t\t\tlabel = "Rename"
\t\t}
\t\tb.WriteString(titleStyle.Render(label) + "\\n\\n")
\t\tb.WriteString(accentStyle.Render("› ") + m.projectNameInput + accentStyle.Render("▌") + "\\n")
\t\tif m.nameError != "" {
\t\t\tb.WriteString("\\n" + badStyle.Render(m.nameError))
\t\t}
\t\tb.WriteString("\\n\\n" + mutedStyle.Render("enter"))
\t\tif m.cfg.Project != "" {
\t\t\tb.WriteString(mutedStyle.Render("  esc"))
\t\t}
\t\treturn b.String()
\t}
'''
assert old in s
s = s.replace(old, new, 1)

# Finished dashboard hint includes rename shortcut.
old = '''\tb.WriteString(mutedStyle.Render("↑/↓  enter  r  q"))
\treturn b.String()
}

func (m model) renderConfirm() string {
'''
new = '''\tb.WriteString(mutedStyle.Render("↑/↓  enter  r  n  q"))
\treturn b.String()
}

func (m model) renderConfirm() string {
'''
assert old in s
s = s.replace(old, new, 1)

p.write_text(s)

# Keep README aligned with the actual TUI instead of the removed helper BAT/wizard.
rp = Path("README.md")
r = rp.read_text()
r = r.replace(
    "For a person signing in from another device, double-click `remote-login.bat`. Google prints an authorization URL; open that URL on the other person's device or paste it into any QR-code generator. After they approve access, paste the verification code back into the terminal. The resulting `gcloud` login is then available to the TUI.\n\n",
    "The TUI supports normal browser sign-in or a terminal QR code for signing in from another device.\n\n",
)
r = r.replace(
    "1. Sign in through the normal `gcloud auth login` browser flow, or use `remote-login.bat` for another-device / QR-style login.\n2. If Google Cloud billing has never been activated for the account, use the billing link shown by the TUI and return when finished.\n3. Review the fixed server shape and press **Create server**.\n4. The TUI creates the project/resources, waits for SSH, installs Hermes as root, and verifies the result.\n",
    "1. Sign in with the browser or terminal QR flow.\n2. Set up/select billing if needed.\n3. Enter a human-readable project name.\n4. The Server screen creates/resumes everything automatically, then becomes the management dashboard.\n",
)
r = r.replace(
    "Afterward, **SSH** opens a normal interactive remote shell. **Hermes** SSHes into the server and launches the root Hermes installation; if Hermes has not been configured yet, it opens `hermes setup` instead.\n",
    "Afterward, **SSH** opens a normal interactive remote shell. **Hermes** launches the root Hermes installation; if Hermes has not been configured yet, it opens `hermes setup` instead. Press `n` on the Server screen to rename the Google Cloud project display name; its permanent `cloud-...` project ID does not change.\n",
)
r = r.replace(
    "The app stores only a small mapping of Google account → managed project in the normal OS config directory under `cloud-charm/config.json`.",
    "The app stores only a small mapping of Google account → managed project/name in the normal OS config directory under `cloud-charm/config.json`.",
)
rp.write_text(r)
