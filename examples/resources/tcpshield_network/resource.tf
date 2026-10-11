resource "tcpshield_network" "main" {
  name = "play.example.com"

  connections_per_second_threshold = 20
  client_ban_seconds               = 120
  vpn_block                        = true
  antivpn_kick_msg                 = "VPNs are not allowed on this server."

  offline_motd_enabled = true
  offline_motd         = "We are down for maintenance."
}

# Point your domain at the network and prove ownership:
output "cname" {
  value = tcpshield_network.main.protected_cname
}

output "txt_record" {
  value = tcpshield_network.main.txt_verification
}
