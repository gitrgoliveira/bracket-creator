# deploy/gcp — GCP Always-Free e2-micro

Deploys the bracket-creator mobile-app on a **GCP Always-Free e2-micro**
(1 GB RAM, x86-64) using Terraform.  Auto-HTTPS via Caddy.

> **Scale ceiling:** GCP's free tier includes **1 GB/month egress from North
> America**.  A single busy tournament day with a few hundred live SSE viewers
> can exceed this.  Overage is billed (~$0.12/GB).  Set a **$1 budget alert**
> (see below) as a tripwire.  For 1000+ viewer events, use the Oracle module.

## Prerequisites

- Terraform >= 1.5 installed locally.
- A GCP project with billing enabled (the Always-Free tier still requires a
  billing account; you will not be charged if you stay within limits).
- `gcloud auth application-default login` completed, or a service-account JSON
  key exported to `GOOGLE_APPLICATION_CREDENTIALS`.
- A domain name with an A record you can update.

## One-command deploy

```bash
cd deploy/gcp

# Copy the example tfvars and fill in your values
cp terraform.tfvars.example terraform.tfvars
$EDITOR terraform.tfvars

terraform init
terraform plan
terraform apply
```

After `apply` completes, the output `instance_public_ip` is shown.  Update
your DNS A record to that IP, then wait for propagation (typically < 5 min).
Caddy will issue the Let's Encrypt certificate automatically on first HTTPS
request — allow ~30 seconds for the ACME challenge to complete.

## terraform.tfvars example

```hcl
project  = "my-gcp-project-id"
region   = "us-central1"         # MUST be us-west1 / us-central1 / us-east1
zone     = "us-central1-a"
hostname = "tournament.example.com"

ssh_pubkey = "ssh-ed25519 AAAA... you@laptop"

# Optional — tighten SSH to your IP only
# operator_cidrs = ["203.0.113.10/32"]

# Optional — reserve a stable IP (free while attached to a running instance)
# reserve_static_ip = true

# Optional — PDF export (heavier image, ~500 MB, more RAM pressure)
# image_ref = "ghcr.io/gitrgoliveira/bracket-creator-mobile-pdf:latest"

# Optional — locked password mode
# lock_password            = true
# tournament_password_hash = "$2b$12$..."
```

## DNS guide

1. Run `terraform apply` and note `instance_public_ip`.
2. In your DNS provider, create/update an A record:
   `tournament.example.com. 300 IN A <instance_public_ip>`
3. Caddy fetches the TLS cert on the first HTTPS request.  Check logs with:
   `gcloud compute ssh <instance-name> -- docker compose -f /opt/app/docker-compose.yaml logs caddy`

**Ephemeral IP caveat:** if `reserve_static_ip = false` (default), the
instance's external IP changes every time you stop and start it.  You must
update the DNS A record after each stop.  Set `reserve_static_ip = true` to
avoid this — the IP is free while the instance is running.

## Watching the egress meter ($1 budget alert)

Set a budget alert in GCP Console → Billing → Budgets & Alerts:

- Budget amount: **$1**
- Scope: this project
- Alert threshold: 50% (to catch creep early)

This is a tripwire, not a hard cap.  GCP does not auto-stop instances on
budget breach.  Check the metric `compute.googleapis.com/instance/network/
sent_bytes_count` in Cloud Monitoring, or navigate to:
  Compute Engine → VM Instances → (instance) → Monitoring → Network tab.

Free egress resets on the 1st of each month.

## Teardown

```bash
terraform destroy
```

This removes the VM, disk, firewall, VPC, and (if created) the static IP.
The static IP costs ~$0.01/hr when unattached — `terraform destroy` removes
it.  A forgotten static IP is the most common surprise-charge vector.

## Backups

Tournament data lives at `/opt/tournament-data` on the VM's 30 GB pd-standard
disk.  Back it up before `terraform destroy`:

```bash
# Ad-hoc: copy to your laptop
gcloud compute scp --recurse \
  <instance-name>:/opt/tournament-data ./tournament-data-backup

# Or snapshot the disk (free within the 30 GB free tier)
gcloud compute disks snapshot <disk-name> \
  --snapshot-names tournament-snap-$(date +%Y%m%d)
```

## Security notes

- **Never commit `terraform.tfstate` or `terraform.tfvars`.** Both can contain
  secrets in plaintext — in particular `tournament_password_hash`. They are
  already covered by `.gitignore`; keep them out of version control.
- For shared or team use, configure a **remote backend** (a GCS bucket) so state
  is encrypted at rest and never stored on a laptop.
- Locking down SSH: set `operator_cidrs` to your own IP range. Ports 80 and 443
  always remain open to the world (viewers and Let's Encrypt need them).

## Module variables

| Variable | Default | Description |
|---|---|---|
| `project` | — | GCP project ID |
| `region` | `us-central1` | Must be us-west1/us-central1/us-east1 |
| `zone` | `""` (auto) | Zone within the region |
| `hostname` | — | FQDN for the app |
| `image_ref` | lean mobile image | Docker image tag |
| `lock_password` | `false` | Enable bcrypt locked mode |
| `tournament_password_hash` | `""` | Bcrypt hash (sensitive) |
| `sse_max_clients` | `5000` | SSE subscriber cap |
| `api_rate_limit` | `500` | Global API requests/sec limit (conservative default; raise for larger events) |
| `api_rate_limit_burst` | `1000` | Global API rate-limit burst |
| `ssh_user` | `ubuntu` | SSH username |
| `ssh_pubkey` | — | SSH public key |
| `operator_cidrs` | `[]` | CIDRs allowed SSH (empty = all) |
| `reserve_static_ip` | `false` | Reserve a stable static IP |

## Outputs

| Output | Description |
|---|---|
| `instance_public_ip` | Public IP — use for DNS A record |
| `app_url` | `https://<hostname>` |
| `ssh_command` | Ready-to-run SSH command |
