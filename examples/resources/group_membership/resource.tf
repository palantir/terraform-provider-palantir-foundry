resource "foundry_group_membership" "example-group-membership" {
  id = foundry_group_declaration.example-group.id
  group_members = ["example-group-id"]
}