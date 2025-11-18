resource "foundry_marking_role_assignments" "example-marking-role-assignments" {
  marking_id = foundry_marking.example-marking.id
  marking_role_assignments = [
    {
      role_id = "ADMINISTER"
      principal_id = "example-user-id"
    },
    {
      role_id = "DECLASSIFY"
      principal_id = "example-user-id"
    },
    {
      role_id = "USE"
      principal_id = "example-group-id"
    }
  ]
}