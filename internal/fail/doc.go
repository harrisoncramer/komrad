// Package fail declares the errors the domain returns, each with the [Kind] of failure it is,
// so rpcerr can pick a status code without a registry of its own.
//
// An [Error] compares with errors.Is like one from errors.New. [Error.Saying] words it for one
// request, such as naming the id that was not found, and that wording reaches the caller without
// the prefixes the domain added on the way up.
package fail
