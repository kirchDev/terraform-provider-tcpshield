# country_id is TCPShield's own numeric id — country_code / country_name show
# what it resolved to.
resource "tcpshield_country_firewall_rule" "andorra" {
  network_id = tcpshield_network.main.id
  country_id = 1
}
