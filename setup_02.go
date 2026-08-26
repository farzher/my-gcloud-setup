package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func ensureChatGPT(cfg config) (commandResult, error) {
	status, err := runTimeout(45*time.Second, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone, "--command=sudo -n -i hermes auth status openai-codex", "--quiet")
	if err != nil || !chatGPTLoggedIn(usefulOutput(status)) {
		return status, errChatGPTAuthRequired
	}
	remote := `sudo -n -i bash -lc 'set -e; hermes config set model.provider openai-codex >/dev/null; hermes config set model.default ` + chatGPTModel + ` >/dev/null; hermes config set agent.reasoning_effort ` + chatGPTEffort + ` >/dev/null; hermes config unset model.base_url >/dev/null 2>&1 || true'`
	configured, err := runTimeout(45*time.Second, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone, "--command="+remote, "--quiet")
	return mergeResult(status, configured), err
}

func chatGPTLoggedIn(output string) bool {
	x := strings.ToLower(output)
	if strings.Contains(x, "not logged") || strings.Contains(x, "not authenticated") || strings.Contains(x, "missing") {
		return false
	}
	return strings.Contains(x, "logged in") || strings.Contains(x, "authenticated")
}

type deployKey struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Key      string `json:"key"`
	ReadOnly bool   `json:"read_only"`
}

func ensureGitHub(cfg config) (config, commandResult, error) {
	var all commandResult
	gh := ghPath()
	if gh == "" {
		return cfg, all, errors.New("GitHub CLI not found; run run.bat again")
	}
	user, ur, err := currentGitHubUser(gh)
	all = mergeResult(all, ur)
	if err != nil || user != githubOwner {
		_, _ = runTimeout(15*time.Second, gh, "auth", "switch", "--hostname", "github.com", "--user", githubOwner)
		user, ur, err = currentGitHubUser(gh)
		all = mergeResult(all, ur)
	}
	if err != nil || user != githubOwner {
		return cfg, all, errGitHubAuthRequired
	}

	if cfg.Repo == "" {
		if existing := discoverServerRepo(cfg); existing != "" {
			if r, viewErr := runTimeout(20*time.Second, gh, "repo", "view", existing, "--json", "isPrivate", "--jq", ".isPrivate"); viewErr == nil && strings.TrimSpace(r.Stdout) == "true" {
				all = mergeResult(all, r)
				cfg.setRepo(cfg.Account, existing)
				if saveErr := saveConfig(cfg); saveErr != nil {
					return cfg, all, saveErr
				}
			}
		}
	}

	if cfg.Repo == "" {
		base := repoSlug(cfg.domainFor(cfg.Account))
		if base == "" {
			base = repoSlug(cfg.nameFor(cfg.Account))
		}
		if base == "" {
			base = "web-server"
		}
		for i := 0; i < 20; i++ {
			name := base
			if i > 0 {
				name = fmt.Sprintf("%s-%d", base, i+1)
			}
			full := githubOwner + "/" + name
			if _, viewErr := runTimeout(20*time.Second, gh, "repo", "view", full, "--json", "name"); viewErr == nil {
				continue
			}
			r, createErr := runTimeout(60*time.Second, gh, "repo", "create", full, "--private", "--disable-issues", "--disable-wiki")
			all = mergeResult(all, r)
			if createErr != nil {
				return cfg, all, createErr
			}
			cfg.setRepo(cfg.Account, full)
			if saveErr := saveConfig(cfg); saveErr != nil {
				return cfg, all, saveErr
			}
			break
		}
		if cfg.Repo == "" {
			return cfg, all, errors.New("could not choose a GitHub repository name")
		}
	} else {
		r, viewErr := runTimeout(20*time.Second, gh, "repo", "view", cfg.Repo, "--json", "isPrivate", "--jq", ".isPrivate")
		all = mergeResult(all, r)
		if viewErr != nil {
			create, createErr := runTimeout(60*time.Second, gh, "repo", "create", cfg.Repo, "--private", "--disable-issues", "--disable-wiki")
			all = mergeResult(all, create)
			if createErr != nil {
				return cfg, all, createErr
			}
		} else if strings.TrimSpace(r.Stdout) != "true" {
			return cfg, all, errors.New("configured GitHub repository is not private")
		}
	}

	pubResult, err := runTimeout(45*time.Second, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone,
		"--command=sudo -n bash -lc 'mkdir -p /root/.ssh; chmod 700 /root/.ssh; test -f /root/.ssh/github-web || ssh-keygen -q -t ed25519 -N \"\" -C \"cloud-web\" -f /root/.ssh/github-web; cat /root/.ssh/github-web.pub'", "--quiet")
	all = mergeResult(all, pubResult)
	if err != nil {
		return cfg, all, err
	}
	pub := lastSSHKey(pubResult.Stdout)
	if pub == "" {
		return cfg, all, errors.New("server deploy key was not returned")
	}
	if err = ensureDeployKey(gh, cfg.Repo, pub); err != nil {
		return cfg, all, err
	}

	remoteURL := "git@github.com:" + cfg.Repo + ".git"
	script := `set -e
mkdir -p /root/.ssh /srv/web
chmod 700 /root/.ssh
ssh-keyscan -H github.com >>/root/.ssh/known_hosts 2>/dev/null || true
sort -u /root/.ssh/known_hosts -o /root/.ssh/known_hosts
cat >/root/.ssh/config <<'SSHCFG'
Host github.com
  HostName github.com
  User git
  IdentityFile /root/.ssh/github-web
  IdentitiesOnly yes
SSHCFG
chmod 600 /root/.ssh/config
if [ ! -d /srv/web/repo/.git ]; then rm -rf /srv/web/repo; git clone ` + shellQuote(remoteURL) + ` /srv/web/repo; fi
git -C /srv/web/repo remote set-url origin ` + shellQuote(remoteURL) + `
git -C /srv/web/repo config user.name Hermes
git -C /srv/web/repo config user.email ` + shellQuote(adminEmail) + `
`
	remoteResult, err := runRemoteBash(cfg, 90*time.Second, script)
	all = mergeResult(all, remoteResult)
	return cfg, all, err
}

func discoverServerRepo(cfg config) string {
	if cfg.Project == "" {
		return ""
	}
	r, err := runTimeout(30*time.Second, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone,
		"--command=sudo -n git -C /srv/web/repo remote get-url origin", "--quiet")
	if err != nil {
		return ""
	}
	url := firstLine(r.Stdout)
	for _, prefix := range []string{"git@github.com:", "ssh://git@github.com/", "https://github.com/"} {
		if strings.HasPrefix(url, prefix) {
			name := strings.TrimSuffix(strings.TrimPrefix(url, prefix), ".git")
			if strings.HasPrefix(name, githubOwner+"/") && !strings.ContainsAny(name, " \t\r\n") {
				return name
			}
		}
	}
	return ""
}

func currentGitHubUser(gh string) (string, commandResult, error) {
	r, err := runTimeout(20*time.Second, gh, "api", "user", "--jq", ".login")
	return firstLine(r.Stdout), r, err
}
