data "tcpshield_domain_preverify" "check" {
  network_id  = data.tcpshield_network.main.id
  domain_name = "play.example.com"
}

output "dns_ready" {
  value = data.tcpshield_domain_preverify.check.verified
}
