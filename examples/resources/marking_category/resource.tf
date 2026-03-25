resource "foundry_marking_category" "example-marking-category" {
  name        = "Example Marking Category"
  description = "An example Marking Category in Foundry"
  initial_permissions = {
    is_public = false
    organization_rids = [
      "ri.multipass..organization.example-organization-id"
    ]
    roles = {
      "ADMINISTER" = ["example-user-id"]
      "VIEW"       = ["example-group-id"]
    }
  }
}
