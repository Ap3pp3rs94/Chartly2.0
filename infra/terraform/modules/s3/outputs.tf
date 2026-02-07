########################
# Object Storage Contract Outputs (provider-neutral)
# Single source of truth: values are sourced from locals in main.tf to prevent drift.
########################

output "contract_version" {
  description = "Version of the provider-neutral object storage contract emitted by this module."
  value       = local.contract_version
}

output "bucket_spec" {
  description = "Provider-neutral bucket spec (canonical contract object)."
  value       = local.bucket_spec
}

output "bucket_spec_versioned" {
  description = "Canonical contract object with embedded contract_version for downstream automation."
  value       = merge(local.bucket_spec, { contract_version = local.contract_version })
}

output "bucket_name" {
  description = "Bucket name."
  value       = local.bucket_spec.bucket_name
}

output "tags" {
  description = "Merged tags/labels map (provider-neutral)."
  value       = local.bucket_tags
}

# Future-facing placeholders (no provider resources here):
output "bucket_id" {
  description = "Provider-specific bucket identifier/ARN/resource id (null until implemented by overlay)."
  value       = null
}

output "bucket_url" {
  description = "Provider-specific bucket URL/endpoint (null until implemented by overlay)."
  value       = null
}

output "replication_targets" {
  description = "Provider-specific replication targets (empty until implemented by overlay)."
  value       = []
}
