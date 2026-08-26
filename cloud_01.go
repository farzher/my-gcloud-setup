package main

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"strings"
)

type config struct {
	Projects map[string]string `json:"projects,omitempty"`
	Names    map[string]string `json:"names,omitempty"`
	Domains  map[string]string `json:"domains,omitempty"`
	Billing  map[string]string `json:"billing,omitempty"`
	Repos    map[string]string `json:"repos,omitempty"`
	Disabled map[string]bool   `json:"disabled,omitempty"`
	Account  string            `json:"-"`
	Project  string            `json:"-"`
	Repo     string            `json:"-"`
}

func (c config) projectFor(account string) string { return c.Projects[account] }

func (c config) nameFor(account string) string { return c.Names[account] }

func (c config) domainFor(account string) string { return c.Domains[account] }

func (c config) billingFor(account string) string { return c.Billing[account] }

func (c config) repoFor(account string) string { return c.Repos[account] }

func (c config) disabledFor(account string) bool { return c.Disabled[account] }

func (c *config) setProject(account, value string) {
	if c.Projects == nil {
		c.Projects = map[string]string{}
	}
	c.Projects[account], c.Account, c.Project = value, account, value
}

func (c *config) setSite(account, name, domain string) {
	if c.Names == nil {
		c.Names = map[string]string{}
	}
	if c.Domains == nil {
		c.Domains = map[string]string{}
	}
	c.Names[account], c.Domains[account] = name, domain
}

func (c *config) setBilling(account, value string) {
	if c.Billing == nil {
		c.Billing = map[string]string{}
	}
	c.Billing[account] = value
}

func (c *config) setRepo(account, value string) {
	if c.Repos == nil {
		c.Repos = map[string]string{}
	}
	c.Repos[account], c.Repo = value, value
}

func (c *config) setDisabled(account string, value bool) {
	if c.Disabled == nil {
		c.Disabled = map[string]bool{}
	}
	if value {
		c.Disabled[account] = true
	} else {
		delete(c.Disabled, account)
	}
}

type billingAccount struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

func (b billingAccount) id() string { return strings.TrimPrefix(b.Name, "billingAccounts/") }

func billingHas(items []billingAccount, id string) bool {
	for _, b := range items {
		if b.id() == id {
			return true
		}
	}
	return false
}

type instanceInfo struct {
	Status            string `json:"status"`
	MachineType       string `json:"machineType"`
	NetworkInterfaces []struct {
		AccessConfigs []struct {
			NatIP string `json:"natIP"`
		} `json:"accessConfigs"`
	} `json:"networkInterfaces"`
}

func (i instanceInfo) ip() string {
	if len(i.NetworkInterfaces) == 0 || len(i.NetworkInterfaces[0].AccessConfigs) == 0 {
		return ""
	}
	return i.NetworkInterfaces[0].AccessConfigs[0].NatIP
}

type cloudState struct {
	Gcloud         bool
	Account        string
	Accounts       []string
	Billing        []billingAccount
	ManagedProject string
	ProjectOK      bool
	ProjectName    string
	VMExists       bool
	Instance       instanceInfo
	StaticIP       string
	SSHReady       bool
	SystemReady    bool
	HermesReady    bool
	ChatGPTReady   bool
	GitHubReady    bool
	WebReady       bool
	DNSReady       bool
	HTTPSReady     bool
	VerifyReady    bool
}

type existingVM struct{ Project, Name, Zone, Status string }

type commandResult struct{ Stdout, Stderr, Command string }

var (
	errChatGPTAuthRequired = errors.New("ChatGPT login required")
	errGitHubAuthRequired  = errors.New("GitHub login required")
	errDNSRequired         = errors.New("DNS record required")
)

func detectCmd(cfg config) tea.Cmd {
	return func() tea.Msg { s, err := detect(cfg); return detectedMsg{s, err} }
}
