resource "foundry_enrollment_role_assignments" "example-enrollment-role-assignments" {
  enrollment_rid = data.foundry_enrollment.example.id
  enrollment_roles = [
    {
      "role_id" : "enrollment:example-role",
      "principal_id" : "example-user-id",
    },
    {
      "role_id" : "enrollment:example-role",
      "principal_id" : "example-group-id",
    },
  ]
}

