// Package documents holds the screen that lists every agent's documents and the document types. The
// screen renders the document under the cursor beside the list, and steps back and forward through
// a document's writes as diffs. A says qualifier in the search finds documents by their text.
//
// The package owns the menus and forms that write a new document, rename or delete documents and
// types, and open a document in the person's editor on the stage.
//
// The root is the host. It opens the screen and renders its list from the parameters the screen
// hands it. The documents screen reaches the host only through the requests it returns, and it
// never imports the root or another domain.
package documents
