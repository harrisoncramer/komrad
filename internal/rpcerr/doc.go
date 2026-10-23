// Package rpcerr turns a handler's failure into the Connect error the caller gets. A domain error
// keeps its kind and answers with the code that kind names, and any other error answers as
// internal. The generated adapters call it, so handlers don't pick a code themselves.
package rpcerr
