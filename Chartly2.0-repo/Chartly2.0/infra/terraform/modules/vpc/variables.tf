variable "name" {
  description = "Logical network name (used for naming and tagging)."
  type        = string

  validation {
    condition     = length(trim(var.name)) > 0 && can(regex("^[a-z0-9-]+$", var.name))
    error_message = "name must be non-empty and contain only lowercase letters, numbers, and hyphens."
  }
}

variable "cidr" {
  description = "Primary IPv4 CIDR block for the network (e.g., 10.0.0.0/16)."
  type        = string

  validation {
    condition     = can(cidrnetmask(var.cidr))
    error_message = "cidr must be a valid IPv4 CIDR block (e.g., 10.0.0.0/16)."
  }
}

variable "enable_ipv6" {
  description = "Whether the target provider implementation should enable IPv6 (if supported)."
  type        = bool
  default     = false
}

variable "az_count" {
  description = "Desired number of availability zones / fault domains to span (provider overlay interprets this)."
  type        = number
  default     = 2

  validation {
    condition     = var.az_count >= 1 && var.az_count <= 6 && floor(var.az_count) == var.az_count
    error_message = "az_count must be an integer between 1 and 6."
  }
}

variable "public_subnet_cidrs" {
  description = "Optional explicit public subnet CIDRs. If empty, provider overlay may compute."
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for c in var.public_subnet_cidrs : can(cidrnetmask(c))])
    error_message = "public_subnet_cidrs must contain only valid IPv4 CIDR blocks."
  }

  validation {
    condition     = length(var.public_subnet_cidrs) == 0 || length(var.public_subnet_cidrs) == var.az_count
    error_message = "public_subnet_cidrs must be empty or have exactly az_count entries."
  }
}

variable "private_subnet_cidrs" {
  description = "Optional explicit private subnet CIDRs. If empty, provider overlay may compute."
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for c in var.private_subnet_cidrs : can(cidrnetmask(c))])
    error_message = "private_subnet_cidrs must contain only valid IPv4 CIDR blocks."
  }

  validation {
    condition     = length(var.private_subnet_cidrs) == 0 || length(var.private_subnet_cidrs) == var.az_count
    error_message = "private_subnet_cidrs must be empty or have exactly az_count entries."
  }
}

variable "tags" {
  description = "Optional free-form tags/labels map for provider overlays."
  type        = map(string)
  default     = {}
}
