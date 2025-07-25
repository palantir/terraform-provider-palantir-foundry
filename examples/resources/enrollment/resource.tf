resource "foundry_enrollment" "example-enrollment" {
  planned_organization_roles = [
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