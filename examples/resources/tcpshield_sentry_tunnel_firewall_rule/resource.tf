data "tcpshield_sentry_tunnel_filters" "all" {}

locals {
  filter = { for f in data.tcpshield_sentry_tunnel_filters.all.filters : f.internal_name => f.id }
}

resource "tcpshield_sentry_tunnel_firewall_rule" "minecraft" {
  tunnel_id  = tcpshield_sentry_tunnel.game.id
  start_port = 25565
  end_port   = 25565
  filter_id  = local.filter["mc"]
}
