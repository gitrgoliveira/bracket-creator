variable "project" {
  description = "GCP project ID."
  type        = string
}

variable "region" {
  description = <<-EOT
    GCP region for the e2-micro instance.  MUST be one of us-west1,
    us-central1, or us-east1 — these are the only regions where the e2-micro
    is part of the Always-Free tier.  Any other region silently incurs charges.
  EOT
  type        = string
  default     = "us-central1"

  validation {
    condition = contains(
      ["us-west1", "us-central1", "us-east1"],
      var.region,
    )
    error_message = <<-EOT
      region must be one of us-west1, us-central1, or us-east1.
      Only those three regions include the e2-micro in GCP's Always-Free tier.
      Deploying to any other region will generate charges.
    EOT
  }
}

variable "zone" {
  description = <<-EOT
    GCP zone within the chosen region, e.g. "us-central1-a".
    Defaults to the -a zone of whichever region is selected.
  EOT
  type        = string
  default     = ""

  # Caller can leave this empty; main.tf uses region + "-a" if blank.
  # (We handle it in the locals rather than here to avoid a complex default.)
}

variable "hostname" {
  description = <<-EOT
    Fully-qualified domain name for the deployment, e.g. "tournament.example.com".
    Caddy uses this to obtain a Let's Encrypt TLS certificate — the DNS A record
    must point at the instance IP before the stack starts.
  EOT
  type        = string

  validation {
    # Must begin with a letter: the hostname is also used to derive GCP
    # resource names (RFC 1035), which may not start with a digit.
    condition     = can(regex("^[a-zA-Z][a-zA-Z0-9.-]*[a-zA-Z0-9]$", var.hostname))
    error_message = "hostname must be a valid FQDN beginning with a letter (letters, digits, dots, and hyphens only — no spaces, newlines, or shell metacharacters)."
  }
}

variable "image_ref" {
  description = <<-EOT
    Docker image to deploy.  Defaults to the lean non-PDF mobile image
    (~20 MB, FROM scratch) which is the natural fit for the 1 GB RAM e2-micro.
    Override to the PDF image only if bracket→PDF export is required on this box:
      ghcr.io/gitrgoliveira/bracket-creator-mobile-pdf:latest
    Note: the PDF image includes LibreOffice (~500 MB) and requires more RAM.
  EOT
  type        = string
  default     = "ghcr.io/gitrgoliveira/bracket-creator-mobile:latest"

  validation {
    condition     = !can(regex("[[:space:]]", var.image_ref))
    error_message = "image_ref must not contain whitespace or newlines."
  }
}

variable "lock_password" {
  description = <<-EOT
    Set to "true" to enable bcrypt locked-mode auth.
    When true, TOURNAMENT_PASSWORD_HASH must also be provided.
    When false, the plaintext password in tournament-data/tournament.md is used.
  EOT
  type        = string
  default     = "false"
}

variable "tournament_password_hash" {
  description = <<-EOT
    Bcrypt hash of the admin password, used when lock_password = "true".
    Generate with: htpasswd -bnBC 12 "" '<password>' | tr -d ':\n'
    This value is written to app.env (chmod 600, root-owned) on the VM.
    Mark as sensitive in your tfvars — never commit the plaintext.
  EOT
  type        = string
  default     = ""
  sensitive   = true
}

variable "sse_max_clients" {
  description = <<-EOT
    Maximum concurrent live-update (SSE) connections. Application default is
    5000. On the e2-micro the practical limit for a public event is the free
    monthly network allowance (1 GB egress) rather than this cap — see README.
  EOT
  type        = number
  default     = 5000
}

variable "api_rate_limit" {
  description = <<-EOT
    Global API rate limit in requests per second across all clients. Requests
    beyond this receive HTTP 429, protecting the instance from traffic spikes.
    The application default is 5000; this conservative default suits a demo or
    small event and can be raised for larger audiences. A built-in per-client
    limit also applies and needs no configuration.
  EOT
  type        = number
  default     = 500
}

variable "api_rate_limit_burst" {
  description = "Burst allowance for the global API rate limit. The application default is 10000."
  type        = number
  default     = 1000
}

variable "ssh_user" {
  description = "Linux username for the SSH public key."
  type        = string
  default     = "ubuntu"
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
    Empty list allows SSH from 0.0.0.0/0 — acceptable for personal use;
    tighten for production deployments.
    Ports 80 and 443 are always open to 0.0.0.0/0.
  EOT
  type        = list(string)
  default     = []
}

variable "reserve_static_ip" {
  description = <<-EOT
    Reserve a static external IP address.  The address is free while attached
    to a running instance; it is billed (~$0.01/hr) if the instance is stopped
    and the IP remains reserved.  Without a static IP, the instance's external
    IP changes on each stop/start — requiring a DNS update and a new TLS cert.
  EOT
  type        = bool
  default     = false
}
