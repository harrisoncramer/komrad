table "forge_connections" {
  schema  = schema.public
  comment = "A forge weave has been connected to, one row per host, holding who the credential was verified as. The credential itself is a file under weave's state directory rather than a column."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "kind" {
    null    = false
    type    = text
    comment = "Which forge the host runs: github or gitlab."
  }
  column "host" {
    null    = false
    type    = text
    comment = "The host agents' remotes name, such as github.com or ghe.example.com. It is unique because one machine acts as one identity on one forge."
  }
  column "api_url" {
    null    = false
    type    = text
    default = ""
    comment = "Where the forge answers API calls when it is not the public one, such as https://ghe.example.com/api/v3. Empty for github.com and gitlab.com."
  }
  column "method" {
    null    = false
    type    = text
    comment = "How the credential was obtained: token for one a person supplied, github_app for one minted from the engine's App."
  }
  column "login" {
    null    = false
    type    = text
    comment = "Who the forge said the credential belongs to when it was verified, a user login or an App slug."
  }
  column "verified_at" {
    null    = true
    type    = timestamptz
    comment = "When the forge last confirmed the credential. Cleared when the forge refuses it, so the row says the connection needs remaking."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  primary_key {
    columns = [column.id]
  }
  unique "forge_connections_host_key" {
    columns = [column.host]
  }
}
