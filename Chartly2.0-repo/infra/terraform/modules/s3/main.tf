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

variable "bucket_name" {
  description = "Bucket name (portable). Portability decision: dots are disallowed. Lowercase letters, numbers, hyphens; length 3..63; must not start/end with hyphen."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9-]{3,63}$", var.bucket_name)) && !can(regex("^-|-$", var.bucket_name))
    error_message = "bucket_name must be 3..63 chars of lowercase letters, numbers, hyphens, and must not start/end with a hyphen."
  }
}

variable "versioning" {
  description = "Object versioning mode."
  type        = string
  default     = "disabled"

  validation {
    condition     = contains(["disabled", "enabled", "suspended"], var.versioning)
    error_message = "versioning must be one of: disabled, enabled, suspended."
  }
}

variable "encryption" {
  description = "Encryption mode (contract enum). Overlays map to provider implementation."
  type        = string
  default     = "managed"

  validation {
    condition     = contains(["none", "managed"], var.encryption)
    error_message = "encryption must be one of: none, managed."
  }
}

variable "force_destroy" {
  description = "Whether to allow destroy of non-empty buckets (overlay/provider interprets)."
  type        = bool
  default     = false
}

variable "exposure_mode" {
  description = "Exposure mode for access: 'private' (default) or 'public'. Public implies anonymous/public-read patterns in overlays."
  type        = string
  default     = "private"

  validation {
    condition     = contains(["private", "public"], var.exposure_mode)
    error_message = "exposure_mode must be one of: private, public."
  }
}

variable "allowed_cidrs" {
  description = "PUBLIC ONLY: Optional IPv4 CIDRs allowed to access the bucket when exposure_mode = 'public'. Must be empty when exposure_mode = 'private'."
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for c in var.allowed_cidrs : can(cidrnetmask(c))])
    error_message = "allowed_cidrs must contain only valid IPv4 CIDR blocks."
  }
}

variable "lifecycle_rules" {
  description = "Optional lifecycle rules (portable shape). Attributes are optional-safe; overlays map to provider-specific lifecycle policies."
  type = list(object({
    id      = string
    enabled = optional(bool, true)

    # Optional expiration for current objects (days)
    expire_days = optional(number)

    # Optional transition for current objects
    transition_days  = optional(number)
    transition_class = optional(string)

    # Optional expiration for noncurrent versions (days)
    noncurrent_expire_days = optional(number)
  }))
  default = []

  validation {
    condition = alltrue([
      for r in var.lifecycle_rules :
      length(trim(r.id)) > 0 &&
      # If set, day fields must be non-negative integers
      (r.expire_days == null || (r.expire_days >= 0 && floor(r.expire_days) == r.expire_days)) &&
      (r.transition_days == null || (r.transition_days >= 0 && floor(r.transition_days) == r.transition_days)) &&
      (r.noncurrent_expire_days == null || (r.noncurrent_expire_days >= 0 && floor(r.noncurrent_expire_days) == r.noncurrent_expire_days)) &&
      # If transition_days is set and > 0, transition_class must be non-empty
      (r.transition_days == null || r.transition_days == 0 || (r.transition_class != null && length(trim(r.transition_class)) > 0)) &&
      # If both transition_days and expire_days are set and > 0, transition should be before expire (portable sanity)
      (
        r.transition_days == null || r.expire_days == null || r.transition_days == 0 || r.expire_days == 0 ||
        r.transition_days < r.expire_days
      )
    ])
    error_message = "Each lifecycle rule must have non-empty id. Optional day fields must be non-negative integers. If transition_days > 0, transition_class must be set. If both transition_days and expire_days are > 0, transition_days must be < expire_days."
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

check "allowed_cidrs_requires_public" {
  assert {
    condition     = !(length(var.allowed_cidrs) > 0 && var.exposure_mode != "public")
    error_message = "Invalid contract: allowed_cidrs may only be set when exposure_mode is 'public'."
  }
}

# Versioning + noncurrent rules correctness: if versioning is disabled, noncurrent_expire_days must not be set (>0) for any rule.
check "noncurrent_requires_versioning" {
  assert {
    condition = !(
      var.versioning == "disabled" &&
      anytrue([for r in var.lifecycle_rules : (r.noncurrent_expire_days != null && r.noncurrent_expire_days > 0)])
    )
    error_message = "Invalid contract: noncurrent_expire_days requires versioning enabled/suspended."
  }
}

########################
# Locals (canonical contract)
########################

locals {
  contract_version = "v1"

  bucket_tags = merge(
    {
      Name      = var.bucket_name
      ManagedBy = "terraform"
      Module    = "object-storage"
    },
    var.tags
  )

  bucket_spec = {
    bucket_name     = var.bucket_name
    versioning      = var.versioning
    encryption      = var.encryption
    force_destroy   = var.force_destroy
    exposure_mode   = var.exposure_mode
    allowed_cidrs   = var.allowed_cidrs
    lifecycle_rules = var.lifecycle_rules
    tags            = local.bucket_tags
  }
}

########################
# Outputs (contract)
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
