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

    expire_days = optional(number)

    transition_days  = optional(number)
    transition_class = optional(string)

    noncurrent_expire_days = optional(number)
  }))
  default = []

  validation {
    condition = alltrue([
      for r in var.lifecycle_rules :
      length(trim(r.id)) > 0 &&
      (r.expire_days == null || (r.expire_days >= 0 && floor(r.expire_days) == r.expire_days)) &&
      (r.transition_days == null || (r.transition_days >= 0 && floor(r.transition_days) == r.transition_days)) &&
      (r.noncurrent_expire_days == null || (r.noncurrent_expire_days >= 0 && floor(r.noncurrent_expire_days) == r.noncurrent_expire_days)) &&
      (r.transition_days == null || r.transition_days == 0 || (r.transition_class != null && length(trim(r.transition_class)) > 0)) &&
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
