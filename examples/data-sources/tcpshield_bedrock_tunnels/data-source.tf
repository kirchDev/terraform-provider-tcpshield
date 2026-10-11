data "tcpshield_bedrock_tunnels" "all" {
  network_id = data.tcpshield_network.main.id
}
