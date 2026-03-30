resource "foundry_organization_guest_membership" "example-organization-guest-membership" {
  organization_rid = "ri.foundry.main.organization.example-organization-rid"
  organization_guest_members = ["example-guest-user-id", "example-guest-group-id"]
}