resource "foundry_organization" "example-organization" {
  name         = "Example organization name"
  description  = "An example organization in Foundry"
  host_name = "example.palantirfoundry.com"
  initial_administrators = ["example-user-id", "example-group-id"]
}