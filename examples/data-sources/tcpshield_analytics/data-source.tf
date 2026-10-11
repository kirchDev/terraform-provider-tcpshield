data "tcpshield_analytics" "users" {
  network_id = data.tcpshield_network.main.id
  metric     = "uniqueUsers"
}

output "unique_users" {
  value = jsondecode(data.tcpshield_analytics.users.json).value
}
