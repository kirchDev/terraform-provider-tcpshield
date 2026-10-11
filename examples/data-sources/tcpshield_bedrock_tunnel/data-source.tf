data "tcpshield_bedrock_tunnel" "bedrock" {
  network_id = data.tcpshield_network.main.id
  id         = 123
}
