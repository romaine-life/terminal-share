# Snapshot storage: one private container, one blob per share (<id>.json). Nothing reads it
# but the service; a share's page reaches it only through GET /api/snapshots/<id>.

resource "azurerm_resource_group" "terminal_share" {
  name     = "terminal-share"
  location = data.azurerm_resource_group.infra.location
}

resource "azurerm_storage_account" "snapshots" {
  name                            = var.storage_account_name
  resource_group_name             = azurerm_resource_group.terminal_share.name
  location                        = azurerm_resource_group.terminal_share.location
  account_tier                    = "Standard"
  account_replication_type        = "LRS"
  account_kind                    = "StorageV2"
  allow_nested_items_to_be_public = false
  # The AzureRM provider uses the storage data plane while managing
  # azurerm_storage_container (as glimmung's artifacts account notes). The service itself
  # signs in with its workload identity, never a key.
  shared_access_key_enabled = true
  min_tls_version           = "TLS1_2"

  tags = {
    app       = "terminal-share"
    managedBy = "terminal-share"
    purpose   = "snapshots"
  }
}

resource "azurerm_storage_container" "snapshots" {
  name                  = "snapshots"
  storage_account_id    = azurerm_storage_account.snapshots.id
  container_access_type = "private"
}

# Deletes each snapshot blob 30 days after it was created. This is the cleanup only: the
# lifecycle run is daily and may lag, and the server refuses a share from its expires_at
# on whether or not the blob is gone yet.
resource "azurerm_storage_management_policy" "snapshots" {
  storage_account_id = azurerm_storage_account.snapshots.id

  rule {
    name    = "delete-expired-snapshots"
    enabled = true
    filters {
      blob_types   = ["blockBlob"]
      prefix_match = ["${azurerm_storage_container.snapshots.name}/"]
    }
    actions {
      base_blob {
        delete_after_days_since_creation_greater_than = var.snapshot_days
      }
    }
  }
}
