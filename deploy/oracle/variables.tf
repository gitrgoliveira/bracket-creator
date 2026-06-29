variable "tenancy_ocid" {
  description = "OCID of your OCI tenancy."
  type        = string
}

variable "compartment_ocid" {
  description = <<-EOT
    OCID of the compartment to deploy into.
    The root compartment OCID equals tenancy_ocid, acceptable for personal
    accounts.  Organisations typically use a dedicated child compartment.
  EOT
  type        = string
}

variable "region" {
  description = <<-EOT
    OCI region identifier, e.g. "us-ashburn-1".
    Always-Free A1 capacity is per-tenancy home region, deploying to your
    home region is recommended.  Availability varies; see README for the
    "Out of capacity" retry note.
  EOT
  type        = string
}

variable "availability_domain" {
  description = <<-EOT
    Availability domain name within the region, e.g. "Uocm:US-ASHBURN-AD-1".
    List ADs: oci iam availability-domain list --compartment-id <tenancy-ocid>
    If one AD is out of A1 capacity, try another in the same region.
  EOT
  type        = string
}

variable "instance_image_ocid" {
  description = <<-EOT
    OCID of the Ubuntu 22.04 Minimal (Arm64) platform image for your region.
    List: oci compute image list --compartment-id <tenancy-ocid> \
           --operating-system "Canonical Ubuntu" \
           --shape VM.Standard.A1.Flex --query 'data[].id'
    OCIDs are region-specific, use the one that matches var.region.
  EOT
  type        = string
}

variable "hostname" {
  description = <<-EOT
    Fully-qualified domain name for the deployment, e.g. "tournament.example.com".
    Caddy uses this to obtain a Let's Encrypt TLS certificate.
  EOT
  type        = string

  validation {
    # Must begin with a letter: the hostname is also used to derive the OCI
    # dns_label / hostname_label, which must start with a letter (RFC 952).
    condition     = can(regex("^[a-zA-Z][a-zA-Z0-9.-]*[a-zA-Z0-9]$", var.hostname))
    error_message = "hostname must be a valid FQDN beginning with a letter (letters, digits, dots, and hyphens only, no spaces, newlines, or shell metacharacters)."
  }
}

variable "image_ref" {
  description = <<-EOT
    Multi-arch Docker image to deploy (must include linux/arm64).
    Defaults to the PDF-capable image, Oracle A1 has 12 GB RAM, so
    LibreOffice headless fits comfortably.
    The arm64 layer is published by the CI workflow (docker-publish.yaml /
    docker-release.yaml) via native ubuntu-24.04-arm runners.
  EOT
  type        = string
  default     = "ghcr.io/gitrgoliveira/bracket-creator-mobile-pdf:latest"

  validation {
    condition     = !can(regex("[[:space:]]", var.image_ref))
    error_message = "image_ref must not contain whitespace or newlines."
  }
}

variable "lock_password" {
  description = <<-EOT
    Set to true to enable bcrypt locked-mode auth.
    When true, tournament_password_hash must also be provided.
  EOT
  type        = bool
  default     = false
}

variable "tournament_password_hash" {
  description = <<-EOT
    Bcrypt hash of the admin password, used when lock_password = true.
    Generate: htpasswd -bnBC 12 "" '<password>' | tr -d ':\n'
    Written to app.env (chmod 600, root-owned) by cloud-init.
    Mark sensitive in your tfvars, never commit the plaintext.
  EOT
  type        = string
  default     = ""
  sensitive   = true
}

variable "sse_max_clients" {
  description = <<-EOT
    Maximum concurrent live-update (SSE) connections. Application default is
    5000, which the Ampere A1 (12 GB RAM) supports comfortably. For very large
    events, validate capacity with a trial run at your expected audience size.
  EOT
  type        = number
  default     = 5000

  validation {
    # The app reads SSE_MAX_CLIENTS with strconv.Atoi (integer-only); a
    # non-integer would be ignored at runtime and silently fall back to the
    # default, so reject it at plan time instead.
    condition     = var.sse_max_clients > 0 && var.sse_max_clients == floor(var.sse_max_clients)
    error_message = "sse_max_clients must be a positive integer."
  }
}

variable "api_rate_limit" {
  description = <<-EOT
    Global API rate limit in requests per second across all clients. Requests
    beyond this receive HTTP 429, protecting the instance from traffic spikes.
    The application default is 5000; this conservative default suits typical
    events and can be raised for larger audiences. A built-in per-client limit
    also applies and needs no configuration.
  EOT
  type        = number
  default     = 1000

  validation {
    # The app parses API_RATE_LIMIT with ParseFloat and ignores values <= 0.
    condition     = var.api_rate_limit > 0
    error_message = "api_rate_limit must be greater than 0."
  }
}

variable "api_rate_limit_burst" {
  description = "Burst allowance for the global API rate limit. The application default is 10000."
  type        = number
  default     = 2000

  validation {
    # The server reads API_RATE_LIMIT_BURST with strconv.Atoi (integer-only).
    condition     = var.api_rate_limit_burst > 0 && var.api_rate_limit_burst == floor(var.api_rate_limit_burst)
    error_message = "api_rate_limit_burst must be a positive integer."
  }
}

variable "ssh_pubkey" {
  description = <<-EOT
    SSH public key to install on the instance (openssh format,
    e.g. "ssh-ed25519 AAAA... user@host").
  EOT
  type        = string
}

variable "operator_cidrs" {
  description = <<-EOT
    List of CIDR ranges allowed to reach port 22 (SSH).
    Empty list allows SSH from 0.0.0.0/0, acceptable for personal use;
    tighten for production.  Ports 80 and 443 are always open to 0.0.0.0/0.
  EOT
  type        = list(string)
  default     = []
}
