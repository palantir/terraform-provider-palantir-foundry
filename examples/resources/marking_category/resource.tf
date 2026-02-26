resource "foundry_marking_category" "example-marking-category" {
  name        = "Example Marking Category"
  description = "An example Marking Category in Foundry"
  initial_permissions = {
    is_public = false
    organization_rids = [
      "ri.multipass..organization.example-organization-id"
    ]
    roles = [
      {
        role         = "ADMINISTER"
        principal_id = "example-user-id"
      },
      {
        role         = "VIEW"
        principal_id = "example-group-id"
      }
    ]
  }
}
