terraform {
  required_providers {
    directus = {
      source  = "Fluent-Health/directus"
      version = "~> 0.1"
    }
  }
}

# The token is a Directus user's static token. Use an admin user: Directus
# hides some configuration (e.g. flows folders) from non-admin users.
# Or set DIRECTUS_URL and DIRECTUS_TOKEN in the environment.
provider "directus" {
  url   = "https://cms.example.com"
  token = var.directus_token
}

variable "directus_token" {
  type      = string
  sensitive = true
}
