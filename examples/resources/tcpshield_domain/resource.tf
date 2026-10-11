resource "tcpshield_domain" "play" {
  network_id     = tcpshield_network.main.id
  name           = "play.example.com"
  backend_set_id = tcpshield_backend_set.lobby.id
}
