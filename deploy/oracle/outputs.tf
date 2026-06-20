output "instance_public_ip" {
  description = <<-EOT
    Reserved public IP address of the Ampere A1 instance.
    Point your DNS A record here.
    This IP is Always-Free and stable across instance stop/start.
  EOT
  value       = oci_core_public_ip.app.ip_address
}

output "app_url" {
  description = "HTTPS URL of the deployed mobile app."
  value       = "https://${var.hostname}"
}

output "ssh_command" {
  description = "SSH command to reach the instance."
  value       = "ssh ubuntu@${oci_core_public_ip.app.ip_address}"
}
