# The API requires all three fields on every update and refuses an empty one.
resource "tcpshield_user_profile" "me" {
  firstname = "Ada"
  lastname  = "Lovelace"
  phone     = "+44 20 7946 0000"
}
