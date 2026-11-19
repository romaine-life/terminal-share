# Workload identity for terminal-share. The pod's ServiceAccount token, projected by the
# azure-workload-identity webhook, is exchanged through the federated credential for this
# identity's Entra token, which may read and write blobs in the snapshots container and
# nothing else.

resource "azurerm_user_assigned_identity" "terminal_share" {
  name                = "terminal-share-identity"
  resource_group_name = azurerm_resource_group.terminal_share.name
  location            = azurerm_resource_group.terminal_share.location
}

resource "azurerm_role_assignment" "snapshots_blob_contributor" {
  scope                = azurerm_storage_container.snapshots.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = azurerm_user_assigned_identity.terminal_share.principal_id
}

# Federated to the terminal-share ServiceAccount in the terminal-share namespace (k8s/).
resource "azurerm_federated_identity_credential" "terminal_share" {
  name                = "aks-terminal-share"
  resource_group_name = azurerm_resource_group.terminal_share.name
  parent_id           = azurerm_user_assigned_identity.terminal_share.id
  audience            = ["api://AzureADTokenExchange"]
  issuer              = local.aks_oidc_issuer_url
  subject             = "system:serviceaccount:terminal-share:terminal-share"
}
