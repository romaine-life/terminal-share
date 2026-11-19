variable "storage_account_name" {
  description = "Globally unique name of the account holding the snapshots (3-24 lowercase letters and digits)."
  type        = string
  default     = "romaineterminalshare"

  validation {
    condition     = can(regex("^[a-z0-9]{3,24}$", var.storage_account_name))
    error_message = "Storage account name must be 3-24 lowercase letters and digits."
  }
}

variable "snapshot_days" {
  description = "Days after creation the lifecycle rule deletes a snapshot blob; the server refuses one from its expires_at on."
  type        = number
  default     = 30
}
