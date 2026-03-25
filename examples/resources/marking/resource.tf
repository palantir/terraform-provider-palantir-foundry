resource "foundry_marking" "example-marking" {
  name        = "Example Marking name"
  description = "Example Marking description"
  category_id = "example-marking-category-id"
  initial_role_assignments = {
    "ADMINISTER" = ["example-user-id"]
    "DECLASSIFY" = ["example-user-id"]
    "USE"        = ["example-group-id"]
  }
}
