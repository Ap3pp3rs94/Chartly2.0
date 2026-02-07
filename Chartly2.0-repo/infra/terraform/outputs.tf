########################
# Root Outputs (provider-neutral)
########################

output "project" {
  description = "Project name."
  value       = var.project
}

output "env" {
  description = "Environment name."
  value       = var.env
}

output "region" {
  description = "Region/locale placeholder (provider overlays may interpret this)."
  value       = var.region
}

output "name_prefix" {
  description = "Common resource name prefix (either var.name_prefix or '<project>-<env>')."
  value       = var.name_prefix != "" ? var.name_prefix : format("%s-%s", var.project, var.env)
}

output "common_tags" {
  description = "Merged common tags/labels (base keys + var.tags). Provider overlays can map/transform these to provider-specific tag models."
  value = merge(
    {
      Project     = var.project
      Environment = var.env
      ManagedBy   = "terraform"
    },
    var.tags
  )
}

# Placeholder pattern: when root begins composing modules, you can project key module outputs here
# for downstream automation (CI/CD, inventory, docs generation). Keep provider-neutral.
#
# Example (later):
# output "module_outputs" {
#   value = {
#     k8s        = module.k8s
#     monitoring = module.monitoring
#   }
# }
#
output "module_outputs" {
  description = "Aggregated module outputs keyed by module name (empty until modules are wired)."
  value       = {}
}
