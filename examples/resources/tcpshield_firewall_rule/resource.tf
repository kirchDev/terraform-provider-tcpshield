# Block a range …
resource "tcpshield_firewall_rule" "block" {
  network_id = tcpshield_network.main.id
  cidr       = "198.51.100.0/24"
}

# … and always let the office through. A single address needs /32.
resource "tcpshield_firewall_rule" "office" {
  network_id = tcpshield_network.main.id
  cidr       = "203.0.113.7/32"
  whitelist  = true
}
