terraform {
  required_version = ">= 1.6.0"

  # Provider constraints should be declared explicitly once you pick an environment/provider,
  # but this root skeleton remains provider-neutral by default.
  #
  # Example pattern (DO NOT enable here unless you are selecting providers in this root module):
  # required_providers {
  #   aws = {
  #     source  = "hashicorp/aws"
  #     version = "~> 5.0"
  #   }
  #   kubernetes = {
  #     source  = "hashicorp/kubernetes"
  #     version = "~> 2.0"
  #   }
  # }

  # Backend is intentionally omitted to avoid vendor assumptions.
  # Configure remote state via per-environment backend config files:
  #   - backend.dev.hcl
  #   - backend.staging.hcl
  #   - backend.prod.hcl
  #
  # Usage:
  #   terraform init -backend-config=backend.dev.hcl
  #
  # Example (S3)  DO NOT enable here:
  # backend "s3" {
  #   # Values come from backend.<env>.hcl via -backend-config
  # }
}

########################
# Input variables pattern
########################

variable "project" {
  description = "Project name."
  type        = string
  default     = "chartly"
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

########################
# Locals pattern
########################

locals {
  project = var.project
  env     = var.env

  name_prefix = "${local.project}-${local.env}"

  # Common tags/labels (provider-neutral concept; map keys can be adapted per provider).
  common_tags = {
    Project     = local.project
    Environment = local.env
    ManagedBy   = "terraform"
  }
}

########################
# Module composition notes
########################
# Recommended directory structure (provider-neutral root):
#   main.tf            - This file (root skeleton)
#   variables.tf       - Inputs (optional split)
#   locals.tf          - Locals (optional split)
#   outputs.tf         - Outputs (optional split)
#   providers.<env>.tf - Provider configuration (environment-specific)
#   backend.<env>.hcl  - Backend config (environment-specific)
#   modules/           - Reusable modules
#
# Compose real infrastructure by calling environment modules, e.g.:
# module "k8s" {
#   source = "./modules/k8s"
#   # inputs...
# }

########################
# Outputs (sanity)
########################

output "chartly_meta" {
  description = "Sanity output to prove Terraform wiring (provider-neutral)."
  value = {
    project     = local.project
    env         = local.env
    name_prefix = local.name_prefix
    common_tags = local.common_tags
  }
}
