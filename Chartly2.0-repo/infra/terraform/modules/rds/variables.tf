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
  description = "Whether DB should be publicly accessible (provider overlay interprets)."
  type        = bool
  default     = false
}

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

variable "enabled_logs" {
  description = "Optional list of log exports to enable (contract enum; compatibility enforced in main.tf via check blocks)."
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
