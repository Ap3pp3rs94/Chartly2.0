########################
# Cluster Contract Outputs (provider-neutral)
# Single source of truth: values are sourced from locals in main.tf to prevent drift.
########################

output "contract_version" {
  description = "Version of the provider-neutral cluster contract emitted by this module."
  value       = local.contract_version
}

output "cluster_spec" {
  description = "Provider-neutral cluster spec (canonical contract object)."
  value       = local.cluster_spec
}

output "cluster_spec_versioned" {
  description = "Canonical contract object with embedded contract_version for downstream automation."
  value       = merge(local.cluster_spec, { contract_version = local.contract_version })
}

output "name" {
  description = "Logical cluster name."
  value       = local.cluster_name
}

output "tags" {
  description = "Merged tags/labels map (provider-neutral)."
  value       = local.common_tags
}

# Future-facing placeholders (no provider resources here):
output "cluster_id" {
  description = "Provider-specific cluster ID/ARN/resource identifier (null until implemented by overlay)."
  value       = null
}

output "kubeconfig" {
  description = "Provider-specific kubeconfig content or reference (null until implemented by overlay)."
  value       = null
}

output "node_group_ids" {
  description = "Provider-specific node group identifiers (empty until implemented by overlay)."
  value       = []
}
