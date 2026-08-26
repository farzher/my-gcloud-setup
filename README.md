# my-gcloud-setup

A tiny Bubble Tea TUI for turning a Google account into a simple Google Compute Engine server and managing it afterward.

It intentionally targets one opinionated setup instead of exposing the whole GCP console:

- `e2-micro` in `us-east1-b`
- latest Debian 13 image family
- 30 GB `pd-standard`
- Premium network tier
- reserved static external IPv4
- SSH, HTTP, and HTTPS firewall access
- no Ops Agent or backup policy configured
- Hermes Agent installed system-wide as root

After provisioning, the TUI provides quick actions for Hermes, SSH, restart/start/stop, rebuild, destroy, and switching Google accounts. Rebuild keeps the reserved static IP. If local `pi` is installed, failed setup steps can optionally be handed to Pi for troubleshooting; AI is not part of the normal provisioning path.

## Requirements

- Go 1.25+
- [Google Cloud CLI (`gcloud`)](https://cloud.google.com/sdk/docs/install)
- A Google account with an active Cloud Billing account
- Optional: [`pi`](https://pi.dev/) for the **Fix with Pi** failure action

## Run on Windows

Double-click `run.bat`. It quietly pulls the latest repo changes and starts the TUI with `go run .`.

The TUI supports normal browser sign-in or a terminal QR code for signing in from another device.

## Build

```sh
git clone https://github.com/farzher/my-gcloud-setup.git
cd my-gcloud-setup
go mod tidy
go build -trimpath -ldflags="-s -w" -o cloud .
```

On Windows you can name the binary `cloud.exe`:

```powershell
go build -trimpath -ldflags="-s -w" -o cloud.exe .
.\cloud.exe
```

## Use

Run `cloud`. On first launch:

1. Sign in with the browser or terminal QR flow.
2. Set up/select billing if needed.
3. Enter a human-readable project name.
4. The Server screen creates/resumes everything automatically, then becomes the management dashboard.

Afterward, **SSH** opens a normal interactive remote shell. **Hermes** launches the root Hermes installation; if Hermes has not been configured yet, it opens `hermes setup` instead. Press `n` on the Server screen to rename the Google Cloud project display name; its permanent `cloud-...` project ID does not change.

## Local state

The app stores only a small mapping of Google account → managed project/name in the normal OS config directory under `cloud-charm/config.json`. Google credentials remain owned by `gcloud`; Hermes credentials remain owned by Hermes.

## Warning

This tool creates and destroys real Google Cloud resources. It is designed around Google Cloud's small Free Tier-compatible VM shape, but Google Cloud pricing and account-specific credits can change. A reserved/attached external IPv4 may be billed separately.