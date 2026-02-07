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
    # Terraform-native CIDR validation (rejects impossible IPs/prefixes).
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

########################
# Locals (deterministic naming + tags)
########################

locals {
  # Keep naming deterministic and portable
  vpc_name = var.name

  # Provider-neutral tag model (overlays map to provider tag requirements)
  common_tags = merge(
    {
      Name      = local.vpc_name
      ManagedBy = "terraform"
      Module    = "vpc"
    },
    var.tags
  )

  # Provider-neutral spec object for automation, docs, and validation layers.
  # Provider overlays should implement resources and then optionally project real IDs back into outputs.
  vpc_spec = {
    name                 = local.vpc_name
    cidr                 = var.cidr
    enable_ipv6          = var.enable_ipv6
    az_count             = var.az_count
    public_subnet_cidrs  = var.public_subnet_cidrs
    private_subnet_cidrs = var.private_subnet_cidrs
    tags                 = local.common_tags
  }
}

########################
# Implementation note (WOW, but still neutral)
########################
# This module defines the CONTRACT only.
# Implementations should live in provider-specific overlays or adapter modules, e.g.:
#   modules/vpc-aws, modules/vpc-azure, modules/vpc-gcp
# Then root composes:
#   module "vpc" { source = "./modules/vpc-aws"; ... }   # in aws env overlay
#
# This keeps the Chartly Terraform tree vendor-neutral while still enabling real infrastructure.

########################
# Outputs (contract)
########################

output "vpc_spec" {
  description = "Provider-neutral VPC/network spec (contract object)."
  value       = local.vpc_spec
}

output "name" {
  description = "Logical VPC/network name."
  value       = local.vpc_name
}

output "tags" {
  description = "Merged tags/labels map (provider-neutral)."
  value       = local.common_tags
}

# Future-facing placeholders (no provider resources here):
# Provider overlays may choose to output real IDs once implemented.
output "vpc_id" {
  description = "Provider-specific VPC/VNet network ID (null until implemented by overlay)."
  value       = null
}

output "public_subnet_ids" {
  description = "Provider-specific public subnet IDs (empty until implemented by overlay)."
  value       = []
}

output "private_subnet_ids" {
  description = "Provider-specific private subnet IDs (empty until implemented by overlay)."
  value       = []
}
