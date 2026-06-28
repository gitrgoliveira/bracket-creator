locals {
  public_ip = (
    var.reserve_static_ip
    ? google_compute_address.static[0].address
    : google_compute_instance.app.network_interface[0].access_config[0].nat_ip
  )
}

output "instance_public_ip" {
  description = <<-EOT
    Public IP address of the e2-micro instance.
    Point your DNS A record here, then Caddy will auto-obtain a TLS cert.
  EOT
  value       = local.public_ip
}

output "app_url" {
  description = "HTTPS URL of the deployed mobile app."
  value       = "https://${var.hostname}"
}

output "ssh_command" {
  description = "SSH command to reach the instance."
  value       = "ssh ${var.ssh_user}@${local.public_ip}"
}
