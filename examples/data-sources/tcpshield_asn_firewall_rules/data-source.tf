data "tcpshield_asn_firewall_rules" "all" {
  network_id = data.tcpshield_network.main.id
}
