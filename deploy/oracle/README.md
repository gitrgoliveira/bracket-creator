# deploy/oracle: Oracle Always-Free Ampere A1

Deploys the bracket-creator mobile-app on an **Oracle Always-Free Ampere A1**
instance (2 OCPU / 12 GB RAM, Arm64) using Terraform.  Auto-HTTPS via Caddy.

> **Current A1 Always-Free cap (2026-06-15 reduction):** Oracle reduced the cap
> from 4 OCPU / 24 GB to **2 OCPU / 12 GB**. Older guides cite 4/24; they
> are stale. This module requests 2 OCPU / 12 GB to stay within Always-Free.

> **Scale target:** Oracle is the only free-forever tier that matches the app's
> 1000+-viewer SSE target.  10 TB/month egress means fan-out is never the cost
> problem here.

## Prerequisites

- Terraform >= 1.5 installed locally.
- An OCI account (free tier) with an API key configured.
- OCI CLI installed and `~/.oci/config` populated (see "OCI provider auth"
  below).
- A domain name with an A record you can update.
- The OCID of an Ubuntu 22.04 Minimal Arm64 platform image in your region.

## OCI provider auth setup

OCI Terraform uses API key auth by default.  One-time setup:

```bash
# 1. Install the OCI CLI
# https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm
oci setup config   # interactive wizard; generates ~/.oci/config + key pair

# 2. Upload the PUBLIC key to the OCI Console
#    Profile (top-right) → My Profile → API Keys → Add API Key
#    Paste the contents of ~/.oci/oci_api_key_public.pem

# 3. Verify
oci iam user get --user-id <your-user-ocid>
```

The Terraform provider reads `~/.oci/config` automatically.  Alternatively,
set these environment variables:

```bash
export OCI_TENANCY_OCID="ocid1.tenancy.oc1..."
export OCI_USER_OCID="ocid1.user.oc1..."
export OCI_FINGERPRINT="aa:bb:cc:..."
export OCI_PRIVATE_KEY_PATH="$HOME/.oci/oci_api_key.pem"
export OCI_REGION="us-ashburn-1"
```

## Finding the Ubuntu Arm64 image OCID

```bash
oci compute image list \
  --compartment-id <tenancy-ocid> \
  --operating-system "Canonical Ubuntu" \
  --operating-system-version "22.04 Minimal aarch64" \
  --shape VM.Standard.A1.Flex \
  --query 'data[0].id' \
  --raw-output
```

OCIDs are region-specific; run this in the same region you intend to deploy.

## One-command deploy

```bash
cd deploy/oracle

# Copy the example tfvars and fill in your values
cp terraform.tfvars.example terraform.tfvars
$EDITOR terraform.tfvars

terraform init
terraform plan
terraform apply
```

After `apply`, the output `instance_public_ip` shows the reserved public IP.
Update your DNS A record to that IP.  Caddy obtains the TLS certificate
automatically on the first HTTPS request (allow ~30 seconds).

## terraform.tfvars example

```hcl
tenancy_ocid     = "ocid1.tenancy.oc1..aaaa..."
compartment_ocid = "ocid1.tenancy.oc1..aaaa..."  # or a child compartment
region           = "us-ashburn-1"
availability_domain = "Uocm:US-ASHBURN-AD-1"

instance_image_ocid = "ocid1.image.oc1.iad.aaaa..."  # Ubuntu 22.04 Minimal aarch64

hostname   = "tournament.example.com"
ssh_pubkey = "ssh-ed25519 AAAA... you@laptop"

# Optional: restrict SSH to your IP
# operator_cidrs = ["203.0.113.10/32"]

# Optional: locked password mode
# lock_password            = true
# tournament_password_hash = "$2b$12$..."
```

## DNS guide

1. Note `instance_public_ip` from `terraform apply` output.
2. In your DNS provider: `tournament.example.com. 300 IN A <public_ip>`
3. The reserved IP is stable; it does not change across instance stop/start, so you only set the DNS record once.
4. Check cert provisioning: `ssh ubuntu@<ip> -- docker compose -f /opt/app/docker-compose.yaml logs caddy`

## The #1 OCI footgun: two firewall layers

Traffic to ports 80/443 must be allowed at **both** levels:

1. **OCI Security List** (opened by Terraform, this module).
2. **In-instance iptables** (Oracle's stock Ubuntu images ship a `iptables` ruleset that **drops 80/443 by default**). The cloud-init script adds accept rules and persists them via `netfilter-persistent`.

If the site times out even though the Security List is open, SSH into the VM
and check: `sudo iptables -L INPUT -n | grep -E '80|443'`

If the accept rules are missing: `sudo iptables -I INPUT 1 -p tcp --dport 443 -j ACCEPT && sudo netfilter-persistent save`

## A1 "Out of capacity": retry note

Oracle A1 Always-Free instances are popular.  If `terraform apply` returns:
```
Out of host capacity.
```

Try:
1. A different availability domain in the same region (`var.availability_domain`).
2. Retry after a few hours; capacity is released as other tenants stop/delete instances.
3. Try a different region (you can move the `tenancy_ocid` home region once).

This is a known Oracle-wide issue, not a module bug.

## Teardown

```bash
terraform destroy
```

Always-Free means no ongoing charges, but destroy to release the A1 capacity
for re-creation and to clean up the VCN/subnet/IGW.

Before destroying, back up tournament data:

```bash
rsync -avz ubuntu@<ip>:/opt/tournament-data ./tournament-data-backup
```

## Backups

OCI block-volume backups are Always-Free within the 5-backup quota.  In the
OCI Console: Storage → Block Volumes → (your boot volume) → Create Manual
Backup.

## Security notes

- **Never commit `terraform.tfstate` or `terraform.tfvars`.** Both can contain secrets in plaintext, in particular `tournament_password_hash`. They are already covered by `.gitignore`; keep them out of version control.
- For shared or team use, configure a **remote backend** (OCI Object Storage) so
  state is encrypted at rest and never stored on a laptop.
- Locking down SSH: set `operator_cidrs` to your own IP range. Ports 80 and 443
  always remain open to the world (viewers and Let's Encrypt need them).

## Module variables

| Variable | Default | Description |
|---|---|---|
| `tenancy_ocid` | (none) | OCI tenancy OCID |
| `compartment_ocid` | (none) | Compartment OCID |
| `region` | (none) | OCI region identifier |
| `availability_domain` | (none) | AD within region |
| `instance_image_ocid` | (none) | Ubuntu 22.04 Arm64 image OCID |
| `hostname` | (none) | FQDN for the app |
| `image_ref` | PDF mobile image | Multi-arch Docker tag |
| `lock_password` | `false` | Enable bcrypt locked mode |
| `tournament_password_hash` | `""` | Bcrypt hash (sensitive) |
| `sse_max_clients` | `5000` | SSE subscriber cap |
| `api_rate_limit` | `1000` | Global API requests/sec limit (conservative default; raise for larger events) |
| `api_rate_limit_burst` | `2000` | Global API rate-limit burst |
| `ssh_pubkey` | (none) | SSH public key |
| `operator_cidrs` | `[]` | CIDRs allowed SSH (empty = all) |

## Outputs

| Output | Description |
|---|---|
| `instance_public_ip` | Reserved public IP (use for DNS A record) |
| `app_url` | `https://<hostname>` |
| `ssh_command` | Ready-to-run SSH command |
