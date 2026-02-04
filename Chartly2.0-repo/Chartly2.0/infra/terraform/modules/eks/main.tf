terraform {
  required_version = ">= 1.6.0"

  # Provider selection belongs in environment overlays.
  # This module is intentionally provider-neutral and defines only the CONTRACT.
  #
  # Example pattern (DO NOT enable here):
  # required_providers {
  #   aws = {
  #     source  = "hashicorp/aws"
  #     version = "~> 5.0"
  #   }
  # }
}

########################
# Inputs (contract)
########################

variable "name" {
  description = "Logical cluster name."
  type        = string

  validation {
    condition     = length(trim(var.name)) > 0 && can(regex("^[a-z0-9-]+$", var.name))
    error_message = "name must be non-empty and contain only lowercase letters, numbers, and hyphens."
  }
}

variable "kubernetes_version" {
  description = "Desired Kubernetes version (FORMAT-ONLY validation). Providers/overlays enforce supported versions. Format: '1.29' or '1.29.x'."
  type        = string
  default     = "1.29"

  validation {
    # Conservative format guard (1.20+). This does not imply provider support for any specific minor.
    condition     = can(regex("^1\\.(2[0-9])(\\.[0-9]+)?$", var.kubernetes_version))
    error_message = "kubernetes_version must look like '1.29' or '1.29.x' (format-only; 1.20+)."
  }
}

variable "endpoint_public_access" {
  description = "Whether the cluster API endpoint should be publicly accessible (provider overlay interprets)."
  type        = bool
  default     = true
}

variable "endpoint_private_access" {
  description = "Whether the cluster API endpoint should be privately accessible (provider overlay interprets)."
  type        = bool
  default     = true
}

variable "api_allowed_cidrs" {
  description = "Optional list of IPv4 CIDRs allowed to access the public API endpoint (empty = provider default/overlay)."
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for c in var.api_allowed_cidrs : can(cidrnetmask(c))])
    error_message = "api_allowed_cidrs must contain only valid IPv4 CIDR blocks."
  }
}

variable "node_groups" {
  description = "Node group contract list (provider overlay maps to managed node groups/ASGs)."
  type = list(object({
    name           = string
    instance_types = list(string)
    min_size       = number
    max_size       = number
    desired_size   = number
    disk_size_gb   = number
    labels         = map(string)
    taints = list(object({
      key    = string
      value  = string
      effect = string # NoSchedule | PreferNoSchedule | NoExecute (validated below)
    }))
  }))
  default = []

  validation {
    condition = alltrue([
      for ng in var.node_groups :
      # Node group identity
      length(trim(ng.name)) > 0 &&
      can(regex("^[a-z0-9-]+$", ng.name)) &&

      # Instance types (non-empty list; no empty/whitespace entries)
      length(ng.instance_types) > 0 &&
      alltrue([for it in ng.instance_types : length(trim(it)) > 0]) &&

      # Scaling correctness + integer semantics
      ng.min_size >= 0 &&
      ng.max_size >= ng.min_size &&
      ng.desired_size >= ng.min_size &&
      ng.desired_size <= ng.max_size &&
      floor(ng.min_size) == ng.min_size &&
      floor(ng.max_size) == ng.max_size &&
      floor(ng.desired_size) == ng.desired_size &&

      # Disk sizing (integer, sane minimum)
      ng.disk_size_gb >= 10 &&
      floor(ng.disk_size_gb) == ng.disk_size_gb &&

      # Labels: minimally enforce non-empty keys (K8s will enforce full syntax downstream)
      alltrue([for k, v in ng.labels : length(trim(k)) > 0]) &&

      # Taints: enforce non-empty key + allowed effect
      alltrue([for t in ng.taints : length(trim(t.key)) > 0]) &&
      alltrue([for t in ng.taints : contains(["NoSchedule", "PreferNoSchedule", "NoExecute"], t.effect)])
    ])
    error_message = "Each node_group must have a safe name, instance_types non-empty (no blank entries), integer sizes (min/max/desired), disk_size_gb >= 10 integer, non-empty label keys, non-empty taint keys, and taint effects in {NoSchedule, PreferNoSchedule, NoExecute}."
  }
}

variable "tags" {
  description = "Optional free-form tags/labels map for provider overlays."
  type        = map(string)
  default     = {}
}

########################
# Contract correctness checks (plan-time; no outputs)
########################

check "endpoint_access" {
  assert {
    condition     = var.endpoint_public_access || var.endpoint_private_access
    error_message = "Invalid contract: at least one of endpoint_public_access or endpoint_private_access must be true."
  }
}

########################
# Locals (canonical contract)
########################

locals {
  contract_version = "v1"

  cluster_name = var.name

  # Tag 'Module' reflects contract type (cluster), not provider folder naming.
  common_tags = merge(
    {
      Name      = local.cluster_name
      ManagedBy = "terraform"
      Module    = "cluster"
    },
    var.tags
  )

  cluster_spec = {
    name                   = local.cluster_name
    kubernetes_version      = var.kubernetes_version
    endpoint_public_access  = var.endpoint_public_access
    endpoint_private_access = var.endpoint_private_access
    api_allowed_cidrs       = var.api_allowed_cidrs
    node_groups             = var.node_groups
    tags                   = local.common_tags
  }
}

########################
# Outputs (contract)
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
