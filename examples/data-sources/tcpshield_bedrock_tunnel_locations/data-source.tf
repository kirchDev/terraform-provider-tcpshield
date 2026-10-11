data "tcpshield_bedrock_tunnel_locations" "all" {
  network_id = data.tcpshield_network.main.id
}
