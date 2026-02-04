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
  description = "Logical cache/Redis name (used for naming and tagging)."
  type        = string

  validation {
    condition     = length(trim(var.name)) > 0 && can(regex("^[a-z0-9-]+$", var.name))
    error_message = "name must be non-empty and contain only lowercase letters, numbers, and hyphens."
  }
}

variable "engine_version" {
  description = "Redis engine version (format-only). Overlays/providers enforce supported versions. Format: '7' or '7.2'."
  type        = string
  default     = "7"

  validation {
    condition     = can(regex("^[0-9]+(\\.[0-9]+)?$", var.engine_version)) && tonumber(split(".", var.engine_version)[0]) >= 4
    error_message = "engine_version must look like '7' or '7.2' and be >= 4 (format-only; support enforced by overlays)."
  }
}

variable "node_type" {
  description = "Node sizing type/class (format-only; overlay/provider interprets)."
  type        = string
  default     = "standard-small"

  validation {
    condition     = length(trim(var.node_type)) > 0
    error_message = "node_type must be a non-empty string."
  }
}

variable "node_count" {
  description = "Number of cache nodes (integer). For clustered/replicated configs, overlays interpret semantics."
  type        = number
  default     = 1

  validation {
    condition     = var.node_count >= 1 && var.node_count <= 50 && floor(var.node_count) == var.node_count
    error_message = "node_count must be an integer between 1 and 50."
  }
}

variable "replicas_per_node" {
  description = "Replicas per primary (integer). Overlays map this to replicas/shards as applicable."
  type        = number
  default     = 0

  validation {
    condition     = var.replicas_per_node >= 0 && var.replicas_per_node <= 5 && floor(var.replicas_per_node) == var.replicas_per_node
    error_message = "replicas_per_node must be an integer between 0 and 5."
  }
}

variable "multi_az" {
  description = "Whether to enable multi-AZ / HA (overlay/provider interprets)."
  type        = bool
  default     = false
}

variable "exposure_mode" {
  description = "Exposure mode for access: 'private' (default) or 'public'. Public requires network placement and allowed_cidrs semantics."
  type        = string
  default     = "private"

  validation {
    condition     = contains(["private", "public"], var.exposure_mode)
    error_message = "exposure_mode must be one of: private, public."
  }
}

########################
# Network placement (provider-neutral)
########################

variable "network_ref" {
  description = "Optional provider-neutral reference to the target network/VPC/VNet (string id, name, or external reference resolved by overlay)."
  type        = string
  default     = ""
}

variable "subnet_refs" {
  description = "Optional provider-neutral subnet references for placement (ids/names resolved by overlay). Empty uses overlay/provider defaults."
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for s in var.subnet_refs : length(trim(s)) > 0])
    error_message = "subnet_refs must not contain empty/whitespace entries."
  }
}

variable "allowed_cidrs" {
  description = "PUBLIC ONLY: Optional IPv4 CIDRs allowed to reach Redis when exposure_mode = 'public'. Must be empty when exposure_mode = 'private'."
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for c in var.allowed_cidrs : can(cidrnetmask(c))])
    error_message = "allowed_cidrs must contain only valid IPv4 CIDR blocks."
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

check "public_requires_network_placement" {
  assert {
    condition     = !(var.exposure_mode == "public" && (var.network_ref == "" && length(var.subnet_refs) == 0))
    error_message = "Invalid contract: exposure_mode 'public' requires network_ref or subnet_refs to be provided."
  }
}

check "allowed_cidrs_requires_public" {
  assert {
    condition     = !(length(var.allowed_cidrs) > 0 && var.exposure_mode != "public")
    error_message = "Invalid contract: allowed_cidrs may only be set when exposure_mode is 'public'."
  }
}

########################
# Locals (canonical contract)
########################

locals {
  contract_version = "v1"

  redis_name = var.name

  # Tag 'Module' reflects contract type (cache), not provider folder naming.
  common_tags = merge(
    {
      Name      = local.redis_name
      ManagedBy = "terraform"
      Module    = "cache"
    },
    var.tags
  )

  redis_spec = {
    name              = local.redis_name
    engine_version    = var.engine_version
    node_type         = var.node_type
    node_count        = var.node_count
    replicas_per_node = var.replicas_per_node
    multi_az          = var.multi_az

    exposure_mode     = var.exposure_mode
    network_ref       = var.network_ref
    subnet_refs       = var.subnet_refs
    allowed_cidrs     = var.allowed_cidrs

    tags              = local.common_tags
  }
}

########################
# Outputs (contract)
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
