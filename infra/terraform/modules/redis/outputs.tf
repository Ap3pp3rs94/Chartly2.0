########################
# Redis/Cache Contract Outputs (provider-neutral)
# Single source of truth: values are sourced from locals in main.tf to prevent drift.
########################

output "contract_version" {
  description = "Version of the provider-neutral Redis/cache contract emitted by this module."
  value       = local.contract_version
}

output "redis_spec" {
  description = "Provider-neutral Redis/cache spec (canonical contract object)."
  value       = local.redis_spec
}

output "redis_spec_versioned" {
  description = "Canonical contract object with embedded contract_version for downstream automation."
  value       = merge(local.redis_spec, { contract_version = local.contract_version })
}

output "name" {
  description = "Logical Redis/cache name."
  value       = local.redis_name
}

output "tags" {
  description = "Merged tags/labels map (provider-neutral)."
  value       = local.common_tags
}

# Future-facing placeholders (no provider resources here):
output "cache_id" {
  description = "Provider-specific cache cluster identifier/ARN/resource id (null until implemented by overlay)."
  value       = null
}

output "endpoint" {
  description = "Primary endpoint/hostname (null until implemented by overlay)."
  value       = null
}

output "port" {
  description = "Cache port (null until implemented by overlay)."
  value       = null
}

output "replica_endpoints" {
  description = "Replica endpoints (empty until implemented by overlay)."
  value       = []
}
