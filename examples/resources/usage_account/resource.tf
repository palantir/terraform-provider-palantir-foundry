resource "foundry_usage_account" "example-usage-account" {
  enrollment_rid = "ri.control-panel.main.customer.example-enrollment-rid"
  display_name   = "Engineering"
  description    = "Tracks usage for the engineering org."
}
