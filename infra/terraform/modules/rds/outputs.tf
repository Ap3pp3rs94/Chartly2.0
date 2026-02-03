########################
# DB Contract Outputs (provider-neutral)
# Single source of truth: values are sourced from locals in main.tf to prevent drift.
########################

output "contract_version" {
  description = "Version of the provider-neutral DB contract emitted by this module."
  value       = local.contract_version
}

output "db_spec" {
  description = "Provider-neutral DB spec (canonical contract object)."
  value       = local.db_spec
}

output "db_spec_versioned" {
  description = "Canonical contract object with embedded contract_version for downstream automation."
  value       = merge(local.db_spec, { contract_version = local.contract_version })
}

output "name" {
  description = "Logical DB name."
  value       = local.db_name
}

output "tags" {
  description = "Merged tags/labels map (provider-neutral)."
  value       = local.common_tags
}

# Future-facing placeholders (no provider resources here):
output "db_id" {
  description = "Provider-specific DB identifier/ARN/resource id (null until implemented by overlay)."
  value       = null
}

output "endpoint" {
  description = "Provider-specific DB endpoint/hostname (null until implemented by overlay)."
  value       = null
}

output "port" {
  description = "Provider-specific DB port (null until implemented by overlay)."
  value       = null
}

output "read_replica_endpoints" {
  description = "Provider-specific read-replica endpoints (empty until implemented by overlay)."
  value       = []
}
