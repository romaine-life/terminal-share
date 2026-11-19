# Shared infrastructure provisioned by infra-bootstrap: the infra resource group's location,
# and the AKS OIDC issuer the federated credential in identity.tf trusts. Read-only;
# terminal-share writes nothing to shared state.

data "azurerm_resource_group" "infra" {
  name = "infra"
}

data "terraform_remote_state" "infra_bootstrap" {
  backend = "azurerm"

  config = {
    resource_group_name  = "infra"
    storage_account_name = "nelsontofu"
    container_name       = "tfstate"
    key                  = "infra-bootstrap.tfstate"
    use_oidc             = true
  }
}

locals {
  aks_oidc_issuer_url = data.terraform_remote_state.infra_bootstrap.outputs.aks_oidc_issuer_url
}
