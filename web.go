package main

import (
	"crypto/sha256"
	"encoding/hex"
)

func buildWebSetupScript(cfg config) string {
	domain := cfg.domainFor(cfg.Account)
	serverName := "_"
	if domain != "" {
		serverName = domain
	}
	return buildWebProvisionScript(cfg, serverName, buildHermesProjectContext(cfg, domain))
}

func contentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
