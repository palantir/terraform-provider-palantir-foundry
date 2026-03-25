resource "foundry_project" "example-project" {
  display_name = "Example Project name"
  space_rid    = "ri.compass.main.folder.example-space-rid"
  initial_organizations = ["example-organization-rid"]
  initial_principal_roles = {
    "example-project-role-id" = {
      groups = ["example-group-id"]
      users  = ["example-user-id"]
    }
  }
}
