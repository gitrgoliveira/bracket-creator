##############################################################################
# bracket-creator — Oracle Always-Free Ampere A1 deployment
#
# Free-forever resources used (current as of 2026-06-15 reduction):
#   - Ampere A1 Flex: 2 OCPU / 12 GB RAM (Arm64)
#   - Boot volume from the 200 GB block-storage Always-Free pool
#   - 10 TB/month egress (effectively unlimited for this workload)
#   - 1 reserved public IP (Always-Free)
#
# A1 is Arm64 — the CI-published multi-arch image must include linux/arm64.
# The VM never builds; it only pulls from GHCR.
#
# WARNING — TWO firewall layers must BOTH be open (§6 in ARCHITECTURE.md):
#   1. OCI Security List (Terraform — this file)
#   2. In-instance iptables (cloud-init — this file)
# Forgetting the iptables rules = security list is open but traffic still
# times out inside the VM.  This is the #1 OCI gotcha.
##############################################################################

terraform {
  required_version = ">= 1.5"

  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.0, < 7.0"
    }
  }
}

# OCI provider reads auth from ~/.oci/config (or env vars — see README).
provider "oci" {
  tenancy_ocid = var.tenancy_ocid
  region       = var.region
}

locals {
  name_prefix = replace(lower(var.hostname), ".", "-")
  data_dir    = "/opt/tournament-data"
  app_dir     = "/opt/app"

  # OCI dns_label / hostname_label are alphanumeric and capped at 15 chars.
  # Strip hyphens and truncate so any real FQDN produces a valid label.
  dns_label = substr(replace(local.name_prefix, "-", ""), 0, 15)

  # Caddyfile rendered from the template in this folder (kept self-contained so
  # the module can be copied and deployed on its own).
  caddyfile = templatefile("${path.module}/Caddyfile.tftpl", {
    hostname = var.hostname
  })
}

# ---------------------------------------------------------------------------
# Networking: VCN, subnet, internet gateway, route table
# ---------------------------------------------------------------------------

resource "oci_core_vcn" "main" {
  compartment_id = var.compartment_ocid
  display_name   = "${local.name_prefix}-vcn"
  cidr_blocks    = ["10.0.0.0/16"]
  dns_label      = local.dns_label
}

resource "oci_core_internet_gateway" "main" {
  compartment_id = var.compartment_ocid
  vcn_id         = oci_core_vcn.main.id
  display_name   = "${local.name_prefix}-igw"
  enabled        = true
}

resource "oci_core_route_table" "public" {
  compartment_id = var.compartment_ocid
  vcn_id         = oci_core_vcn.main.id
  display_name   = "${local.name_prefix}-rt"

  route_rules {
    destination       = "0.0.0.0/0"
    destination_type  = "CIDR_BLOCK"
    network_entity_id = oci_core_internet_gateway.main.id
  }
}

resource "oci_core_security_list" "public" {
  compartment_id = var.compartment_ocid
  vcn_id         = oci_core_vcn.main.id
  display_name   = "${local.name_prefix}-sl"

  # Outbound: allow all (needed for Docker pull, Let's Encrypt ACME, etc.)
  egress_security_rules {
    protocol    = "all"
    destination = "0.0.0.0/0"
  }

  # Inbound: HTTPS
  ingress_security_rules {
    protocol = "6" # TCP
    source   = "0.0.0.0/0"
    tcp_options {
      min = 443
      max = 443
    }
  }

  # Inbound: HTTP (Let's Encrypt ACME HTTP-01 challenge + redirect)
  ingress_security_rules {
    protocol = "6"
    source   = "0.0.0.0/0"
    tcp_options {
      min = 80
      max = 80
    }
  }

  # Inbound: SSH (restrict to operator CIDRs when provided)
  dynamic "ingress_security_rules" {
    for_each = (
      length(var.operator_cidrs) > 0
      ? var.operator_cidrs
      : ["0.0.0.0/0"]
    )
    content {
      protocol = "6"
      source   = ingress_security_rules.value
      tcp_options {
        min = 22
        max = 22
      }
    }
  }

  # Inbound: ICMP type 3 code 4 (fragmentation needed) — required for path MTU
  # discovery so large responses are not silently dropped. Matches OCI's own
  # default security-list template.
  ingress_security_rules {
    protocol = "1" # ICMP
    source   = "0.0.0.0/0"
    icmp_options {
      type = 3
      code = 4
    }
  }

  # Inbound: ICMP type 3 (all codes) from within the VCN — destination
  # unreachable / network diagnostics between hosts in the network.
  ingress_security_rules {
    protocol = "1"
    source   = "10.0.0.0/16"
    icmp_options {
      type = 3
    }
  }
}

resource "oci_core_subnet" "public" {
  compartment_id    = var.compartment_ocid
  vcn_id            = oci_core_vcn.main.id
  display_name      = "${local.name_prefix}-subnet"
  cidr_block        = "10.0.1.0/24"
  route_table_id    = oci_core_route_table.public.id
  security_list_ids = [oci_core_security_list.public.id]
  dns_label         = "public"
}

# Reserved public IP — Always-Free; stable across instance stop/start.
# This means the DNS A record (and Let's Encrypt cert) survive reboots.
resource "oci_core_public_ip" "app" {
  compartment_id = var.compartment_ocid
  lifetime       = "RESERVED"
  display_name   = "${local.name_prefix}-ip"

  # Attach to the VNIC after the instance is created (see lifecycle below).
  private_ip_id = data.oci_core_private_ips.app_vnic.private_ips[0].id

  lifecycle {
    ignore_changes = [private_ip_id]
  }
}

# Discover the primary VNIC private IP so we can attach the reserved public IP.
data "oci_core_vnic_attachments" "app" {
  compartment_id = var.compartment_ocid
  instance_id    = oci_core_instance.app.id
}

data "oci_core_vnic" "app" {
  vnic_id = data.oci_core_vnic_attachments.app.vnic_attachments[0].vnic_id
}

data "oci_core_private_ips" "app_vnic" {
  vnic_id = data.oci_core_vnic.app.id
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
      - iptables-persistent
      - netfilter-persistent

    runcmd:
      # --- Install Docker (official repo, arm64) ---
      - install -m 0755 -d /etc/apt/keyrings
      - |
        curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
          | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
      - chmod a+r /etc/apt/keyrings/docker.gpg
      - |
        echo "deb [arch=$(dpkg --print-architecture) \
          signed-by=/etc/apt/keyrings/docker.gpg] \
          https://download.docker.com/linux/ubuntu \
          $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
          > /etc/apt/sources.list.d/docker.list
      - apt-get update -y
      - >
        apt-get install -y
        docker-ce docker-ce-cli containerd.io
        docker-buildx-plugin docker-compose-plugin
      - systemctl enable --now docker

      # --- Open in-instance iptables for 80 and 443 ---
      # OCI stock Ubuntu images ship a restrictive iptables that DROPS 80/443
      # even when the OCI Security List allows them.  Both layers must be open.
      - iptables -I INPUT 1 -p tcp --dport 80 -j ACCEPT
      - iptables -I INPUT 1 -p tcp --dport 443 -j ACCEPT
      # Persist across reboots via iptables-persistent / netfilter-persistent
      - netfilter-persistent save

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

      # --- Pull image and start the stack ---
      # No build tooling on the VM — pull only.
      - cd ${local.app_dir} && docker compose pull
      - cd ${local.app_dir} && docker compose up -d
  CLOUDINIT
}

# ---------------------------------------------------------------------------
# Compute instance — Ampere A1 Flex (Arm64)
# ---------------------------------------------------------------------------

resource "oci_core_instance" "app" {
  compartment_id      = var.compartment_ocid
  availability_domain = var.availability_domain
  display_name        = local.name_prefix
  shape               = "VM.Standard.A1.Flex"

  shape_config {
    # 2 OCPU / 12 GB is the current Always-Free Ampere A1 cap (as of 2026-06-15).
    # Oracle reduced this from 4/24; do not exceed 2/12 to stay free.
    ocpus         = 2
    memory_in_gbs = 12
  }

  source_details {
    # Ubuntu 22.04 Minimal for Arm64 — use the OCID from your tenancy's region.
    # List OCIDs: oci compute image list --compartment-id <tenancy-ocid>
    #             --operating-system "Canonical Ubuntu" --shape VM.Standard.A1.Flex
    source_type = "image"
    source_id   = var.instance_image_ocid

    # Boot volume — keep on the Always-Free boot volume pool.
    boot_volume_size_in_gbs = 50
  }

  create_vnic_details {
    subnet_id        = oci_core_subnet.public.id
    display_name     = "${local.name_prefix}-vnic"
    assign_public_ip = false # We use a reserved IP; see oci_core_public_ip
    hostname_label   = local.dns_label
  }

  metadata = {
    ssh_authorized_keys = var.ssh_pubkey
    user_data           = base64encode(local.cloud_init)
  }

  lifecycle {
    # Fail at plan/apply rather than at container startup: locked mode with an
    # empty hash makes the app exit immediately (it fails closed).
    precondition {
      condition     = lower(trimspace(var.lock_password)) != "true" || trimspace(var.tournament_password_hash) != ""
      error_message = "tournament_password_hash must be set when lock_password is \"true\" — the app fails closed and exits at startup with an empty hash."
    }

    ignore_changes = [
      # Prevent Terraform from recreating when cloud-init changes post-deploy.
      metadata["user_data"],
      # Image OCIDs can change (security patches); ignore after initial create.
      source_details[0].source_id,
    ]
  }
}
