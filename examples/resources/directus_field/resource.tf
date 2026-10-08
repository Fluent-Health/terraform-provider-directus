resource "directus_field" "title" {
  collection    = directus_collection.articles.collection
  field         = "title"
  type          = "string"
  max_length    = 200
  nullable      = false
  default_value = jsonencode("untitled")
  interface     = "input"
  options       = jsonencode({ trim = true })
  translations  = jsonencode([{ language = "en-US", translation = "Title" }])
}

# Fields with no column are "alias"; their kind goes in `special`.
resource "directus_field" "seo" {
  collection = directus_collection.articles.collection
  field      = "seo"
  type       = "alias"
  special    = ["alias", "no-data", "group"]
  interface  = "group-detail"
}

resource "directus_field" "seo_title" {
  collection = directus_collection.articles.collection
  field      = "seo_title"
  type       = "string"
  group      = directus_field.seo.field
}
