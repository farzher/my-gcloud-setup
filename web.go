package main

func buildWebSetupScript(cfg config) string {
	domain := cfg.domainFor(cfg.Account)
	serverName := "_"
	if domain != "" {
		serverName = domain
	}
	return buildWebProvisionScript(cfg, serverName, buildHermesProjectContext(cfg, domain))
}
