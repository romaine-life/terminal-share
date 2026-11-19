# Remote state in Azure Storage (backend config passed by CI: <repo>.tfstate in the
# nelsontofu account's tfstate container). Authentication uses Forgejo OIDC through this
# repo's app service principal.

terraform {
  backend "azurerm" {
    use_oidc         = true
    use_azuread_auth = true
  }
}

provider "azurerm" {
  features {}
  use_oidc = true
}
