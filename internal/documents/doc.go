// Package documents holds the rules for the markdown documents agents write for people to read,
// and for the document types that say what each kind of document holds.
//
// An agent writes only its own documents, and a rewrite names the version it replaces. Only the
// person deletes a document or writes a type. When the person writes an agent's document, the
// agent gets mail saying so.
package documents
