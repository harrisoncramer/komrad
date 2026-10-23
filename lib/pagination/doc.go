// Package pagination turns a request's page token and limit into the cursor and bound a query runs
// with, and turns the records that come back into the token for the next page.
//
// A resource takes part by implementing [Cursorable], returning the cursor that places it among
// its peers. The token is base64 over the JSON cursor, so callers pass it back unchanged and its
// contents stay the server's business. A cursor's Validate says which values it accepts, so a token
// holding anything else is refused before the query runs.
package pagination
