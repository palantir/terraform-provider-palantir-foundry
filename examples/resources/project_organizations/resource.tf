resource "foundry_project_organizations" "example-project-organizations" {
  project_rid = foundry_project.example-project.rid
  project_organizations = ["example-organization-rid"]
}