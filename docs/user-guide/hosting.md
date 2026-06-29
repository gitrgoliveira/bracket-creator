# Host the live app

The `mobile-app` command serves plain **HTTP**. To run a fully digital event (on-screen scoreboards and competitor result pages reachable over the internet) you put it behind a reverse proxy that terminates HTTPS. The ready-made deployments under [`deploy/`](https://github.com/gitrgoliveira/bracket-creator/tree/main/deploy) all use [Caddy](https://caddyserver.com/) for automatic Let's Encrypt certificates, so HTTPS works with no manual certificate steps.

You need:

- A **domain name** with an A record pointing at the host's public IP.
- Ports **80 and 443** open (port 80 is required for the ACME HTTP-01 challenge that issues the certificate).

## Choose a deployment

Each option links to its full guide on **GitHub**, where the Compose and Terraform files live alongside the instructions.

| Option | Platform | Scale | Use when |
|---|---|---|---|
| [Docker baseline](https://github.com/gitrgoliveira/bracket-creator/tree/main/deploy/docker) | Any Linux host with Docker | Depends on the host | You already have a server or VM and want the provider-agnostic Compose + Caddy stack. |
| [GCP free tier](https://github.com/gitrgoliveira/bracket-creator/tree/main/deploy/gcp) | GCP Always-Free e2-micro (1 GB RAM, x86-64), Terraform-deployed | Club-sized events; 1 GB/month egress cap | You want a free cloud VM. Watch the egress cap: a busy day with a few hundred live viewers can exceed it. |
| [Oracle free tier](https://github.com/gitrgoliveira/bracket-creator/tree/main/deploy/oracle) | Oracle Always-Free Ampere A1 (2 OCPU / 12 GB, Arm64), Terraform-deployed | 1000+ live viewers; 10 TB/month egress | You need the free-forever tier that matches the app's large-event SSE target. |

The Docker baseline is the reusable core: the GCP and Oracle Terraform modules render the same Compose file and Caddyfile onto a cloud VM using cloud-init. Each guide includes the full prerequisites and a one-command Terraform (or Compose) flow.

!!! note "PDF export needs the PDF-enabled image"
    The lean default image cannot generate PDFs (competitor tags, name sheets, trees), it ships without LibreOffice to stay small. If your operators export PDFs, run the `ghcr.io/gitrgoliveira/bracket-creator-mobile-pdf:latest` image variant instead. Excel export works on every image.

## Security

Any deployment reachable over the internet should run in **locked-password mode** rather than the default file mode. Locked mode reads a bcrypt hash from an environment variable and disables the public password-reset endpoint. See [Admin authentication](mobile-app.md) in the mobile app guide for the setup steps.

## What still needs printing

A fully digital setup still leaves one job on paper: organisers print player tags and numbers before the event. Everything else (pools, scoring, scoreboards, and result pages) runs on screen. See the [three ways to run a tournament](../index.md#three-ways-to-run-a-tournament) for how this mode compares to the offline and partially connected setups.
