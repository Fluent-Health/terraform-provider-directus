# A fixed id keeps the folder identical in every environment, so settings and
# field options that reference it can be copied across unchanged.
resource "directus_folder" "assets" {
  id   = "6f1c3a52-6a3e-4b6e-9f0e-2d7c4a1b8e90"
  name = "Assets"
}

resource "directus_folder" "images" {
  name   = "Images"
  parent = directus_folder.assets.id
}

resource "directus_folder" "automations" {
  name = "Automations"
  type = "flows"
}
