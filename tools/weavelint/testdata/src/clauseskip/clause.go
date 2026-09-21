package clauseskip

// timeout bounds each look at a directory, since a dead mount hangs.
const timeout = 1

// retries is how many times a call is made, because the first can fail.
const retries = 2

// limit caps the page, so that a client never reads the whole table.
const limit = 3

// wide is one of the shared groups, rather than one of a screen's.
const wide = 4

// host is loopback, which is why the endpoint carries no auth.
const host = "localhost"

// split names the address a flag carries,
// since it is a flag as often as a setting.
const split = 5

/* block bounds the look, since a mount can hang. */
const block = 6

// plain says what the thing is and stops.
const plain = 7

// restrictive is the row, which is the one the person typed.
const restrictive = 8

// aside holds since and because as words, so it reads as prose.
const aside = 9

func body() int {
	// pick is the first, since the rest are copies.
	pick := 1

	return pick + timeout + retries + limit + wide + split + block + plain + restrictive + aside + len(host)
}
