package clause

// timeout bounds each look at a directory, since a dead mount hangs. // want `comment justifies itself with ", since": delete the clause, or say it in a sentence of its own`
const timeout = 1

// retries is how many times a call is made, because the first can fail. // want `comment justifies itself with ", because"`
const retries = 2

// limit caps the page, so that a client never reads the whole table. // want `comment justifies itself with ", so that"`
const limit = 3

// wide is one of the shared groups, rather than one of a screen's. // want `comment justifies itself with ", rather than"`
const wide = 4

// host is loopback, which is why the endpoint carries no auth. // want `comment justifies itself with ", which is why"`
const host = "localhost"

// split names the address a flag carries,
// since it is a flag as often as a setting. // want `comment justifies itself with ", since"`
const split = 5

/* block bounds the look, since a mount can hang. */ // want `comment justifies itself with ", since"`
const block = 6

// plain says what the thing is and stops.
const plain = 7

// restrictive is the row, which is the one the person typed.
const restrictive = 8

// aside holds since and because as words, so it reads as prose.
const aside = 9

func body() int {
	// pick is the first, since the rest are copies. // want `comment justifies itself with ", since"`
	pick := 1

	return pick + timeout + retries + limit + wide + split + block + plain + restrictive + aside + len(host)
}
