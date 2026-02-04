########################
# VPC Contract Outputs (provider-neutral)
# Single source of truth: values are sourced from locals in main.tf to prevent drift.
########################

output "contract_version" {
  description = "Version of the provider-neutral VPC contract emitted by this module."
  value       = "v1"
}

output "vpc_spec" {
  description = "Provider-neutral VPC/network spec (canonical contract object)."
  value       = local.vpc_spec
}

output "vpc_spec_versioned" {
  description = "Canonical contract object with embedded contract_version for downstream automation."
  value       = merge(local.vpc_spec, { contract_version = "v1" })
}

output "name" {
  description = "Logical VPC/network name."
  value       = local.vpc_name
}

output "tags" {
  description = "Merged tags/labels map (provider-neutral)."
  value       = local.common_tags
}

# Future-facing placeholders (no provider resources here):
# Provider overlays may choose to output real IDs once implemented.
output "vpc_id" {
  description = "Provider-specific VPC/VNet network ID (null until implemented by overlay)."
  value       = null
}

output "public_subnet_ids" {
  description = "Provider-specific public subnet IDs (empty until implemented by overlay)."
  value       = []
}

output "private_subnet_ids" {
  description = "Provider-specific private subnet IDs (empty until implemented by overlay)."
  value       = []
}
