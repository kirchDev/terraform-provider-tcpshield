data "tcpshield_sentry_tunnel_bind_port" "free" {}

resource "tcpshield_sentry_tunnel" "game" {
  name          = "game-server"
  endpoint_peer = "203.0.113.20"
  bind_port     = data.tcpshield_sentry_tunnel_bind_port.free.port

  # The suggested port changes on every read; keep the one the tunnel got.
  lifecycle {
    ignore_changes = [bind_port]
  }
}

data "tcpshield_sentry_tunnel_setup_script" "game" {
  tunnel_id = tcpshield_sentry_tunnel.game.id
}
