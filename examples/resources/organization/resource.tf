resource "foundry_organization" "example-organization" {
  name         = "Example organization nmae"
  description  = "An example organization in Foundry"
  host_name = "example.palantirfoundry.com"
  organization_members = ["example-user-id", "example-group-id"]
  organization_roles = [
    {
      "role_id" : "organization:example-role",
      "principal_id" : "example-user-id",
    },
    {
      "role_id" : "organization:example-role",
      "principal_id" : "example-group-id",
    },
  ]
}