data "tcpshield_sentry_tunnel" "game" {
  id = 42
}

output "monthly_bytes" {
  value = data.tcpshield_sentry_tunnel.game.month_bandwidth_usage
}
