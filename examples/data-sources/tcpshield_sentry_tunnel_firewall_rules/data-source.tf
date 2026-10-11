data "tcpshield_sentry_tunnel_firewall_rules" "game" {
  tunnel_id = data.tcpshield_sentry_tunnel.game.id
}
