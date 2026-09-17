package tfvars

import (
	"testing"
)

// TestSetKeepsThePaddingTheFileUses covers a variables file a person aligned by hand, where
// rewriting the line rather than the value reformats something nobody asked to change.
func TestSetKeepsThePaddingTheFileUses(t *testing.T) {
	const file = "ami_id                       = \"ami-old\"\nname = \"weave\"\n"

	got, changed, err := set(file, "ami_id", "ami-new")
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Fatal("a different value is a change")
	}

	const want = "ami_id                       = \"ami-new\"\nname = \"weave\"\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestAddRefusesToRepeatItself covers running the task twice, which would otherwise write the
// same address in twice and have the cloud refuse the duplicate rule.
func TestAddRefusesToRepeatItself(t *testing.T) {
	const file = "ssh_allowed_cidrs = [\"1.1.1.1/32\"]\n"

	first, changed, err := add(file, "ssh_allowed_cidrs", "2.2.2.2/32")
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Fatal("a new address is a change")
	}

	if first != "ssh_allowed_cidrs = [\"1.1.1.1/32\", \"2.2.2.2/32\"]\n" {
		t.Fatalf("got %q", first)
	}

	_, changed, err = add(first, "ssh_allowed_cidrs", "2.2.2.2/32")
	if err != nil {
		t.Fatal(err)
	}

	if changed {
		t.Fatal("an address already in the list is not a change")
	}
}

// TestAddReadsAListSpreadOverLines covers the shape terraform files are often written in, which
// is what a line-based tool would miss.
func TestAddReadsAListSpreadOverLines(t *testing.T) {
	const file = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\",\n  \"3.3.3.3/32\",\n]\n"

	_, changed, err := add(file, "ssh_allowed_cidrs", "3.3.3.3/32")
	if err != nil {
		t.Fatal(err)
	}

	if changed {
		t.Fatal("the address is already there, across lines")
	}
}

// TestMissingNameIsAnError covers a caller naming something the file does not hold, which must
// not quietly leave the file as it was.
func TestMissingNameIsAnError(t *testing.T) {
	if _, _, err := set("name = \"weave\"\n", "ami_id", "x"); err == nil {
		t.Fatal("a name that is not there is an error")
	}

	if _, _, err := add("name = \"weave\"\n", "ssh_allowed_cidrs", "x"); err == nil {
		t.Fatal("a name that is not there is an error")
	}
}
