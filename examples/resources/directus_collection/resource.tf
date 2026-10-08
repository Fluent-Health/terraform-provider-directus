# A group: organises collections in the data model, has no table.
resource "directus_collection" "content" {
  collection = "content"
  table      = false
  icon       = "folder"
}

resource "directus_collection" "articles" {
  collection  = "articles"
  primary_key = { type = "uuid" }
  group       = directus_collection.content.collection
  icon        = "article"
  translations = jsonencode([
    { language = "en-US", translation = "Articles", singular = "Article", plural = "Articles" },
  ])

  # Destroying a table collection drops the table and every row. Leave this
  # false; set it to true and apply only when you really mean to drop it.
  allow_destroy = false
}
