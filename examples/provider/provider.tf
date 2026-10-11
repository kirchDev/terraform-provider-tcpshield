terraform {
  required_providers {
    tcpshield = {
      source = "kirchdev/tcpshield"
    }
  }
}

# The API key may also come from TCPSHIELD_API_KEY.
provider "tcpshield" {
  api_key = var.tcpshield_api_key
}

variable "tcpshield_api_key" {
  type      = string
  sensitive = true
}
