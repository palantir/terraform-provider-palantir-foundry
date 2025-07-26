resource "foundry_enrollment" "example-enrollment" {
  planned_enrollment_roles = [
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