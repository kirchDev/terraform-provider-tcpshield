resource "tcpshield_backend_set" "lobby" {
  network_id     = tcpshield_network.main.id
  name           = "lobby"
  backends       = ["203.0.113.10:25565", "203.0.113.11:25565"]
  proxy_protocol = true
}
