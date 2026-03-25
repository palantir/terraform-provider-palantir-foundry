resource "foundry_enrollment_role_assignments" "example-enrollment-role-assignments" {
  enrollment_rid = data.foundry_enrollment.example.id
  enrollment_role_assignments = {
    "enrollment:example-role" = ["example-user-id", "example-group-id"],
    "enrollment:second-example-role" = ["example-group-id"],
  }
}
