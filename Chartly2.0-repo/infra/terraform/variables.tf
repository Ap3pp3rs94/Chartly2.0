variable "project" {
  description = "Project name."
  type        = string
  default     = "chartly"

  validation {
    condition     = length(trim(var.project)) > 0 && can(regex("^[a-z0-9-]+$", var.project))
    error_message = "project must be non-empty and contain only lowercase letters, numbers, and hyphens."
  }
}

variable "env" {
  description = "Environment name (dev/staging/prod)."
  type        = string
  default     = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.env)
    error_message = "env must be one of: dev, staging, prod."
  }
}

variable "region" {
  description = "Optional region/locale placeholder for provider overlays (e.g., us-east-1). Use 'local' for provider-neutral defaults."
  type        = string
  default     = "local"

  validation {
    condition     = var.region == "local" || length(trim(var.region)) > 0
    error_message = "region must be 'local' or a non-empty region string."
  }
}

variable "name_prefix" {
  description = "Optional explicit name prefix override. If empty, locals should compute '<project>-<env>'."
  type        = string
  default     = ""

  validation {
    condition     = var.name_prefix == "" || can(regex("^[a-z0-9-]+$", var.name_prefix))
    error_message = "name_prefix must be empty or contain only lowercase letters, numbers, and hyphens."
  }
}

variable "tags" {
  description = "Optional free-form tags/labels map for provider overlays."
  type        = map(string)
  default     = {}
}
