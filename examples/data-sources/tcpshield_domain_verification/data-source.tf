data "tcpshield_domain_verification" "play" {
  network_id = tcpshield_domain.play.network_id
  domain_id  = tcpshield_domain.play.id
}
