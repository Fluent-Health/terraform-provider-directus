# Many-to-one articles.author -> authors, with the one-to-many
# authors.articles listing the articles that point back.
resource "directus_field" "author" {
  collection = "articles"
  field      = "author"
  type       = "uuid"
  special    = ["m2o"]
  interface  = "select-dropdown-m2o"
}

resource "directus_field" "authors_articles" {
  collection = "authors"
  field      = "articles"
  type       = "alias"
  special    = ["o2m"]
  interface  = "list-o2m"
}

resource "directus_relation" "author" {
  collection         = directus_field.author.collection
  field              = directus_field.author.field
  related_collection = "authors"
  one_field          = directus_field.authors_articles.field
  on_delete          = "SET NULL"
}
