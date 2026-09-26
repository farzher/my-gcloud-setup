package main

import (
	"crypto/sha256"
	"encoding/hex"
)

const webManagedHashFile = "/var/lib/website/.managed-web-hash"

func buildWebSetupBody(cfg config) string {
	domain := cfg.domainFor(cfg.Account)
	serverName := "_"
	if domain != "" {
		serverName = domain
	}
	return buildWebProvisionScript(cfg, serverName, buildHermesProjectContext(cfg, domain))
}

func webManagedHash(cfg config) string {
	return contentHash(buildWebSetupBody(cfg))
}

func buildWebSetupScript(cfg config) string {
	body := buildWebSetupBody(cfg)
	return body + "printf '%s\\n' " + shellQuote(contentHash(body)) + " >" + shellQuote(webManagedHashFile) + "\n"
}

func contentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
