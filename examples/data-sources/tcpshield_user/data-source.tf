data "tcpshield_user" "me" {}

output "plan" {
  value = data.tcpshield_user.me.plan_name
}
