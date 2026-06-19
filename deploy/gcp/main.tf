##############################################################################
# bracket-creator — GCP Always-Free e2-micro deployment
#
# Free-forever resources used:
#   - 1× e2-micro (us-west1 / us-central1 / us-east1 ONLY)
#   - 30 GB pd-standard boot disk
#   - 1 GB/month egress free (North America) — see README for the scale ceiling
#
# !! Region validation is a billing guard — an e2-micro outside the three US
#    free regions is NOT free and will generate charges. !!
##############################################################################

terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0, < 7.0"
    }
  }
}

provider "google" {
  project = var.project
  region  = var.region
}

locals {
  # Sanitise hostname for use as a resource-name prefix (RFC 1035).
  name_prefix = replace(lower(var.hostname), ".", "-")

  # Default zone to region-a when the caller omits the variable.
  effective_zone = (
    var.zone != ""
    ? var.zone
    : "${var.region}-a"
  )

  # The app writes to /opt/tournament-data on the VM; compose bind-mounts it.
  data_dir = "/opt/tournament-data"
  app_dir  = "/opt/app"

  # Caddyfile rendered from the template in this folder (kept self-contained so
  # the module can be copied and deployed on its own).
  caddyfile = templatefile("${path.module}/Caddyfile.tftpl", {
    hostname = var.hostname
  })
}

# ---------------------------------------------------------------------------
# Networking
# ---------------------------------------------------------------------------

resource "google_compute_network" "default" {
  name                    = "${local.name_prefix}-net"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "default" {
  name          = "${local.name_prefix}-subnet"
  region        = var.region
  network       = google_compute_network.default.self_link
  ip_cidr_range = "10.0.0.0/24"
}

# Web traffic (HTTP/HTTPS) is ALWAYS open to the world: viewers come from
# anywhere, and Caddy's Let's Encrypt HTTP-01 challenge requires inbound port
# 80 from any IP. This is intentionally separate from the SSH rule so that
# restricting SSH never restricts public web access.
resource "google_compute_firewall" "allow_web" {
  name    = "${local.name_prefix}-allow-web"
  network = google_compute_network.default.self_link

  allow {
    protocol = "tcp"
    ports    = ["80", "443"]
  }

  source_ranges = ["0.0.0.0/0"]
}

# SSH is restricted to operator CIDRs when provided; otherwise open to all
# (acceptable for personal deployments, tighten for production).
resource "google_compute_firewall" "allow_ssh" {
  name    = "${local.name_prefix}-allow-ssh"
  network = google_compute_network.default.self_link

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = (
    length(var.operator_cidrs) > 0
    ? var.operator_cidrs
    : ["0.0.0.0/0"]
  )
}

# Optional reserved static IP — costs nothing while attached to a running
# instance.  Use it so the DNS A record never needs updating after a
# stop/start cycle.
resource "google_compute_address" "static" {
  count  = var.reserve_static_ip ? 1 : 0
  name   = "${local.name_prefix}-ip"
  region = var.region
}

# ---------------------------------------------------------------------------
# Cloud-init user-data
# ---------------------------------------------------------------------------

locals {
  cloud_init = <<-CLOUDINIT
    #cloud-config
    packages:
      - ca-certificates
      - curl
      - gnupg

    runcmd:
      # --- Install Docker (official repo) ---
      - install -m 0755 -d /etc/apt/keyrings
      - |
        curl -fsSL https://download.docker.com/linux/debian/gpg \
          | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
      - chmod a+r /etc/apt/keyrings/docker.gpg
      - |
        echo "deb [arch=$(dpkg --print-architecture) \
          signed-by=/etc/apt/keyrings/docker.gpg] \
          https://download.docker.com/linux/debian \
          $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
          > /etc/apt/sources.list.d/docker.list
      - apt-get update -y
      - >
        apt-get install -y
        docker-ce docker-ce-cli containerd.io
        docker-buildx-plugin docker-compose-plugin
      - systemctl enable --now docker

      # --- Data directory (uid 65534 = nonroot in the container image) ---
      - mkdir -p ${local.data_dir}
      - chown -R 65534:65534 ${local.data_dir}

      # --- App directory ---
      - mkdir -p ${local.app_dir}

      # --- docker-compose.yaml ---
      - |
        cat > ${local.app_dir}/docker-compose.yaml <<'EOF'
        services:
          app:
            image: ${var.image_ref}
            restart: unless-stopped
            env_file:
              - ${local.app_dir}/app.env
            environment:
              - TOURNAMENT_DATA_DIR=/tournament-data
            volumes:
              - ${local.data_dir}:/tournament-data
            # The app never needs the cloud metadata service; map its hostname
            # to loopback so a compromised container cannot read instance
            # metadata (which includes the cloud-init user-data).
            extra_hosts:
              - "metadata.google.internal:127.0.0.1"
            expose:
              - "8080"

          caddy:
            image: caddy:2-alpine
            restart: unless-stopped
            ports:
              - "80:80"
              - "443:443"
            volumes:
              - ${local.app_dir}/Caddyfile:/etc/caddy/Caddyfile:ro
              - caddy_data:/data
              - caddy_config:/config
            depends_on:
              - app

        volumes:
          caddy_data:
          caddy_config:
        EOF

      # --- Caddyfile (rendered from Caddyfile.tftpl) ---
      - echo ${base64encode(local.caddyfile)} | base64 -d > ${local.app_dir}/Caddyfile

      # --- app.env (chmod 600 — contains secrets) ---
      - |
        cat > ${local.app_dir}/app.env <<'EOF'
        LOCK_PASSWORD=${var.lock_password}
        TOURNAMENT_PASSWORD_HASH=${var.tournament_password_hash}
        SSE_MAX_CLIENTS=${var.sse_max_clients}
        API_RATE_LIMIT=${var.api_rate_limit}
        API_RATE_LIMIT_BURST=${var.api_rate_limit_burst}
        EOF
      - chmod 600 ${local.app_dir}/app.env
      - chown root:root ${local.app_dir}/app.env

      # --- Start the stack ---
      - cd ${local.app_dir} && docker compose pull
      - cd ${local.app_dir} && docker compose up -d
  CLOUDINIT
}

# ---------------------------------------------------------------------------
# Compute instance
# ---------------------------------------------------------------------------

resource "google_compute_instance" "app" {
  name         = local.name_prefix
  machine_type = "e2-micro"
  zone         = local.effective_zone

  # Debian 12 (Bookworm) — keep in sync with Dockerfile.mobile base image
  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-12"
      size  = 30 # GiB — free-tier pd-standard cap
      type  = "pd-standard"
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.default.self_link

    access_config {
      nat_ip = (
        var.reserve_static_ip
        ? google_compute_address.static[0].address
        : null
      )
    }
  }

  metadata = {
    user-data              = local.cloud_init
    ssh-keys               = "${var.ssh_user}:${var.ssh_pubkey}"
    block-project-ssh-keys = "true"
  }

  tags = ["${local.name_prefix}-web"]

  # No service account is attached: the app only needs anonymous outbound
  # HTTPS to pull the public GHCR image, which requires no GCP credential.
  # Omitting the block avoids handing the instance a broad API token.

  lifecycle {
    # Fail at plan/apply rather than at container startup: locked mode with an
    # empty hash makes the app exit immediately (it fails closed).
    precondition {
      condition     = lower(trimspace(var.lock_password)) != "true" || trimspace(var.tournament_password_hash) != ""
      error_message = "tournament_password_hash must be set when lock_password is \"true\" — the app fails closed and exits at startup with an empty hash."
    }

    ignore_changes = [
      # Prevent Terraform from recreating the instance when cloud-init
      # user-data changes after initial deploy.
      metadata["user-data"],
    ]
  }
}
