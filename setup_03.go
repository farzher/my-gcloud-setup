package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func ensureDeployKey(gh, repo, pub string) error {
	r, err := runTimeout(30*time.Second, gh, "api", "repos/"+repo+"/keys")
	if err != nil {
		return err
	}
	var keys []deployKey
	if err = json.Unmarshal([]byte(r.Stdout), &keys); err != nil {
		return err
	}
	title, want := "cloud-web-server", keyCore(pub)
	for _, k := range keys {
		if keyCore(k.Key) == want && !k.ReadOnly {
			return nil
		}
	}
	for _, k := range keys {
		if k.Title == title || keyCore(k.Key) == want {
			_, _ = runTimeout(20*time.Second, gh, "api", "--method", "DELETE", fmt.Sprintf("repos/%s/keys/%d", repo, k.ID))
		}
	}
	_, err = runTimeout(30*time.Second, gh, "api", "--method", "POST", "repos/"+repo+"/keys", "-f", "title="+title, "-f", "key="+pub, "-F", "read_only=false")
	return err
}

func keyCore(k string) string {
	f := strings.Fields(strings.TrimSpace(k))
	if len(f) < 2 {
		return strings.TrimSpace(k)
	}
	return f[0] + " " + f[1]
}

func lastSSHKey(s string) string {
	lines := nonEmptyLines(s)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "ssh-") {
			return lines[i]
		}
	}
	return ""
}

func setupWeb(cfg config) (commandResult, error) {
	return runRemoteBash(cfg, 10*time.Minute, buildWebSetupScript(cfg))
}
