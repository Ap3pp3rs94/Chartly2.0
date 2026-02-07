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
  description = "Logical database name (used for naming and tagging)."
  type        = string

  validation {
    condition     = length(trim(var.name)) > 0 && can(regex("^[a-z0-9-]+$", var.name))
    error_message = "name must be non-empty and contain only lowercase letters, numbers, and hyphens."
  }
}

variable "engine" {
  description = "Database engine (contract enum). Provider overlay maps to engine family."
  type        = string
  default     = "postgres"

  validation {
    condition     = contains(["postgres", "mysql", "mariadb"], var.engine)
    error_message = "engine must be one of: postgres, mysql, mariadb."
  }
}

variable "engine_major_version" {
  description = "Engine major version (HYGIENE validation only). Overlays/providers enforce supported versions (e.g., '15' for Postgres)."
  type        = string
  default     = "15"

  validation {
    condition     = can(regex("^[0-9]{1,3}$", var.engine_major_version)) && tonumber(var.engine_major_version) >= 1
    error_message = "engine_major_version must be a numeric string between 1 and 999 (support enforced by overlays)."
  }
}

variable "instance_class" {
  description = "Instance sizing class (format-only; provider overlay interprets, e.g., 'db.t4g.micro' or 'standard-small')."
  type        = string
  default     = "standard-small"

  validation {
    condition     = length(trim(var.instance_class)) > 0
    error_message = "instance_class must be a non-empty string."
  }
}

variable "storage_gb" {
  description = "Allocated storage in GB (contract range)."
  type        = number
  default     = 20

  validation {
    condition     = var.storage_gb >= 10 && var.storage_gb <= 16384 && floor(var.storage_gb) == var.storage_gb
    error_message = "storage_gb must be an integer between 10 and 16384."
  }
}

variable "multi_az" {
  description = "Whether to deploy in multi-AZ / HA mode (provider overlay interprets)."
  type        = bool
  default     = false
}

variable "publicly_accessible" {
  description = "Whether DB should be publicly accessible (provider overlay interprets). Requires network placement inputs (see network_ref/subnet_refs/allowed_cidrs)."
  type        = bool
  default     = false
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
  description = "PUBLIC ACCESS ONLY: Optional IPv4 CIDRs allowed to reach the DB when publicly_accessible is true (empty = overlay/provider default). Must be empty when publicly_accessible is false."
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for c in var.allowed_cidrs : can(cidrnetmask(c))])
    error_message = "allowed_cidrs must contain only valid IPv4 CIDR blocks."
  }
}

########################
# Backups & maintenance
########################

variable "backup_retention_days" {
  description = "Backup retention days (0 disables backups; contract range)."
  type        = number
  default     = 7

  validation {
    condition     = var.backup_retention_days >= 0 && var.backup_retention_days <= 35 && floor(var.backup_retention_days) == var.backup_retention_days
    error_message = "backup_retention_days must be an integer between 0 and 35."
  }
}

variable "backup_window" {
  description = "Preferred backup window in UTC (format HH:MM-HH:MM, 24h). Empty uses provider default. Contract allows cross-midnight; overlays may further constrain."
  type        = string
  default     = ""

  validation {
    condition     = var.backup_window == "" || can(regex("^([01][0-9]|2[0-3]):[0-5][0-9]-([01][0-9]|2[0-3]):[0-5][0-9]$", var.backup_window))
    error_message = "backup_window must be empty or match HH:MM-HH:MM (24h)."
  }
}

variable "maintenance_window" {
  description = "Preferred maintenance window (format ddd:HH:MM-ddd:HH:MM). Empty uses provider default. Contract allows cross-day; overlays may further constrain."
  type        = string
  default     = ""

  validation {
    condition     = var.maintenance_window == "" || can(regex("^(mon|tue|wed|thu|fri|sat|sun):([01][0-9]|2[0-3]):[0-5][0-9]-(mon|tue|wed|thu|fri|sat|sun):([01][0-9]|2[0-3]):[0-5][0-9]$", var.maintenance_window))
    error_message = "maintenance_window must be empty or match ddd:HH:MM-ddd:HH:MM (e.g., sun:03:00-sun:04:00)."
  }
}

########################
# Logging (engine-aware contract)
########################

variable "enabled_logs" {
  description = "Optional list of log exports to enable (contract enum; validated for basic engine compatibility)."
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for l in var.enabled_logs : contains(["general", "error", "slowquery", "postgresql", "upgrade"], l)])
    error_message = "enabled_logs entries must be one of: general, error, slowquery, postgresql, upgrade."
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

# If backups are disabled, backup_window must not be set.
check "backup_window_requires_backups" {
  assert {
    condition     = !(var.backup_retention_days == 0 && var.backup_window != "")
    error_message = "Invalid contract: backup_window cannot be set when backup_retention_days is 0."
  }
}

# If publicly accessible, require at least some placement contract (subnets or network ref).
check "public_requires_network_placement" {
  assert {
    condition     = !(var.publicly_accessible && (var.network_ref == "" && length(var.subnet_refs) == 0))
    error_message = "Invalid contract: publicly_accessible requires network_ref or subnet_refs to be provided."
  }
}

# allowed_cidrs is PUBLIC ACCESS ONLY.
check "allowed_cidrs_requires_public" {
  assert {
    condition     = !(length(var.allowed_cidrs) > 0 && !var.publicly_accessible)
    error_message = "Invalid contract: allowed_cidrs may only be set when publicly_accessible is true."
  }
}

# Engine compatibility for enabled_logs (minimal, portable constraints):
# - postgresql log export only makes sense for postgres
check "logs_engine_compat_postgresql" {
  assert {
    condition     = !(contains(var.enabled_logs, "postgresql") && var.engine != "postgres")
    error_message = "Invalid contract: enabled_logs includes 'postgresql' but engine is not postgres."
  }
}

# - slowquery/general are mysql-family only in this contract (overlays may extend, but contract stays strict)
check "logs_engine_compat_mysql_family" {
  assert {
    condition = !(
      (contains(var.enabled_logs, "slowquery") || contains(var.enabled_logs, "general")) &&
      var.engine == "postgres"
    )
    error_message = "Invalid contract: enabled_logs includes 'slowquery' or 'general' with engine postgres."
  }
}

########################
# Locals (canonical contract)
########################

locals {
  contract_version = "v1"

  db_name = var.name

  # Tag 'Module' reflects contract type (db), not provider folder naming.
  common_tags = merge(
    {
      Name      = local.db_name
      ManagedBy = "terraform"
      Module    = "db"
    },
    var.tags
  )

  db_spec = {
    name                  = local.db_name
    engine                = var.engine
    engine_major_version  = var.engine_major_version
    instance_class        = var.instance_class
    storage_gb            = var.storage_gb
    multi_az              = var.multi_az
    publicly_accessible   = var.publicly_accessible

    network_ref           = var.network_ref
    subnet_refs           = var.subnet_refs
    allowed_cidrs         = var.allowed_cidrs

    backup_retention_days = var.backup_retention_days
    backup_window         = var.backup_window
    maintenance_window    = var.maintenance_window

    enabled_logs          = var.enabled_logs
    tags                  = local.common_tags
  }
}

########################
# Outputs (contract)
########################

output "contract_version" {
  description = "Version of the provider-neutral DB contract emitted by this module."
  value       = local.contract_version
}

output "db_spec" {
  description = "Provider-neutral DB spec (canonical contract object)."
  value       = local.db_spec
}

output "db_spec_versioned" {
  description = "Canonical contract object with embedded contract_version for downstream automation."
  value       = merge(local.db_spec, { contract_version = local.contract_version })
}

output "name" {
  description = "Logical DB name."
  value       = local.db_name
}

output "tags" {
  description = "Merged tags/labels map (provider-neutral)."
  value       = local.common_tags
}

# Future-facing placeholders (no provider resources here):
output "db_id" {
  description = "Provider-specific DB identifier/ARN/resource id (null until implemented by overlay)."
  value       = null
}

output "endpoint" {
  description = "Provider-specific DB endpoint/hostname (null until implemented by overlay)."
  value       = null
}

output "port" {
  description = "Provider-specific DB port (null until implemented by overlay)."
  value       = null
}

output "read_replica_endpoints" {
  description = "Provider-specific read-replica endpoints (empty until implemented by overlay)."
  value       = []
}
