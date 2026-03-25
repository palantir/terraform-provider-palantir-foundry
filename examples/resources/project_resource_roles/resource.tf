resource "foundry_project_resource_roles" "example-project-resource-roles" {
  project_rid = foundry_project.example-project.rid
  principal_roles = {
    "example-project-role-id" = {
      groups = ["example-group-id"]
      users  = ["example-user-id"]
    }
  }
  default_roles = ["example-project-role-id"]
}
