resource "tcpshield_asn_firewall_rule" "hosting" {
  network_id = tcpshield_network.main.id
  asn        = 64496
}
