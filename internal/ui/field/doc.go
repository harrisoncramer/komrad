// Package field holds the panel's text fields. These are the one-row line a person types into, the
// name typed over a row in place, and the vim keys over a form's fields.
//
// A field keeps its own text and cursor and knows nothing about the screen it sits on. The search
// line, the forms and the screens build on it. The package imports nothing under internal/ui.
package field
