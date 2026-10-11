resource "tcpshield_bedrock_tunnel" "bedrock" {
  network_id     = tcpshield_network.main.id
  name           = "bedrock"
  backend_set_id = tcpshield_backend_set.lobby.id
  location       = "FRA"
}

output "bedrock_address" {
  value = tcpshield_bedrock_tunnel.bedrock.allocated_cname
}
