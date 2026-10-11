data "tcpshield_domains" "all" {
  network_id = data.tcpshield_network.main.id
}
