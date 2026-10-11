data "tcpshield_backend_set" "lobby" {
  network_id = data.tcpshield_network.main.id
  id         = 123456
}
