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
      effect = string # NoSchedule | PreferNoSchedule | NoExecute
    }))
  }))
  default = []

  validation {
    condition = alltrue([
      for ng in var.node_groups :
      length(trim(ng.name)) > 0 &&
      can(regex("^[a-z0-9-]+$", ng.name)) &&

      length(ng.instance_types) > 0 &&
      alltrue([for it in ng.instance_types : length(trim(it)) > 0]) &&

      ng.min_size >= 0 &&
      ng.max_size >= ng.min_size &&
      ng.desired_size >= ng.min_size &&
      ng.desired_size <= ng.max_size &&
      floor(ng.min_size) == ng.min_size &&
      floor(ng.max_size) == ng.max_size &&
      floor(ng.desired_size) == ng.desired_size &&

      ng.disk_size_gb >= 10 &&
      floor(ng.disk_size_gb) == ng.disk_size_gb &&

      alltrue([for k, v in ng.labels : length(trim(k)) > 0]) &&

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
