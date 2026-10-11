data "tcpshield_domain" "play" {
  network_id = data.tcpshield_network.main.id
  name       = "play.example.com"
}
