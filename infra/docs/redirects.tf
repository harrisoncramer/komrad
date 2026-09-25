data "cloudflare_zone" "redirect" {
  for_each = toset(var.redirect_domain_names)

  filter = {
    name = each.value
  }
}

locals {
  redirect_records = merge([
    for domain in var.redirect_domain_names : {
      for name in [domain, "www.${domain}"] : name => domain
    }
  ]...)
}

resource "cloudflare_dns_record" "redirect" {
  for_each = local.redirect_records

  zone_id = data.cloudflare_zone.redirect[each.value].zone_id
  name    = each.key
  type    = "AAAA"
  content = "100::"
  ttl     = 1
  proxied = true
}

resource "cloudflare_ruleset" "redirect" {
  for_each = toset(var.redirect_domain_names)

  zone_id = data.cloudflare_zone.redirect[each.value].zone_id
  name    = "Redirect to ${var.domain_name}"
  kind    = "zone"
  phase   = "http_request_dynamic_redirect"

  rules = [{
    ref         = "redirect_to_${replace(var.domain_name, ".", "_")}"
    description = "Send every request to ${var.domain_name}"
    expression  = "true"
    action      = "redirect"
    action_parameters = {
      from_value = {
        status_code           = 301
        preserve_query_string = true
        target_url = {
          expression = "concat(\"https://${var.domain_name}\", http.request.uri.path)"
        }
      }
    }
  }]
}
