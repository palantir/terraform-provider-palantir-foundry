resource "foundry_marking_role_assignments" "example-marking-role-assignments" {
  marking_id = foundry_marking.example-marking.id
  marking_role_assignments = {
    "ADMINISTER" = ["example-user-id"]
    "DECLASSIFY" = ["example-user-id"]
    "USE"        = ["example-group-id"]
  }
}
