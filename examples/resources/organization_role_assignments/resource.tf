resource "foundry_organization_role_assignments" "example-organization-role-assignments" {
  organization_rid = foundry_organization.example-organization.id
  organization_role_assignments = {
    "organization:example-role" = ["example-user-id", "example-group-id"],
    "organization:second-example-role" = ["example-group-id"],
  }
}
