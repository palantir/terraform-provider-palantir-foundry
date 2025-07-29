resource "foundry_marking" "example-marking" {
  name = "Example marking name"
  category_id="example-marking-category-id"
  marking_members = ["example-user-id", "example-group-id"]
  marking_roles = [
    {
      role = "ADMINISTER"
      principal_id="example-user-id"
    },
    {
      role = "USE"
      principal_id="example-group-id"
    }
  ]
}