output "terminal_share_identity_client_id" {
  value       = azurerm_user_assigned_identity.terminal_share.client_id
  description = "client_id of terminal-share-identity: k8s/values.yaml serviceAccount.clientId."
}

output "storage_account_url" {
  value       = azurerm_storage_account.snapshots.primary_blob_endpoint
  description = "The blob endpoint: k8s/values.yaml storage.accountUrl."
}

output "snapshots_container" {
  value       = azurerm_storage_container.snapshots.name
  description = "The container holding one <id>.json blob per share: k8s/values.yaml storage.container."
}
