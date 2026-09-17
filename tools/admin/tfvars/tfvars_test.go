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

// TestAddKeepsTheShapeTheFileWasWrittenIn covers a list somebody broke over lines, where
// collapsing it onto one makes every later diff the whole variable rather than the added line.
func TestAddKeepsTheShapeTheFileWasWrittenIn(t *testing.T) {
	const file = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\",\n  \"3.3.3.3/32\",\n]\n"
	const want = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\",\n  \"3.3.3.3/32\",\n  \"9.9.9.9/32\",\n]\n"

	got, changed, err := add(file, "ssh_allowed_cidrs", "9.9.9.9/32")
	if err != nil {
		t.Fatal(err)
	}

	if !changed || got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestAddLeavesACommentWithItsOwnEntry covers annotating which address is which office, where
// writing the new entry ahead of the comment silently reassigns what it describes, and writing
// it into the line produces a list Terraform cannot parse.
func TestAddLeavesACommentWithItsOwnEntry(t *testing.T) {
	const file = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\", # home\n]\n"
	const want = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\", # home\n  \"9.9.9.9/32\",\n]\n"

	got, changed, err := add(file, "ssh_allowed_cidrs", "9.9.9.9/32")
	if err != nil {
		t.Fatal(err)
	}

	if !changed || got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestAddIsNotFooledByACommentNamingTheValue covers the membership test reading past comments,
// since an address mentioned in one is not an entry.
func TestAddIsNotFooledByACommentNamingTheValue(t *testing.T) {
	const file = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\", # not 9.9.9.9/32\n]\n"

	got, changed, err := add(file, "ssh_allowed_cidrs", "9.9.9.9/32")
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Fatal("a comment naming the value is not the value being in the list")
	}

	if _, changed, _ = add(got, "ssh_allowed_cidrs", "9.9.9.9/32"); changed {
		t.Fatal("the second run has nothing to do")
	}
}

// TestAddClosesAnEntryThatHadNoComma covers a last entry written without a trailing comma,
// which needs one before anything can follow it.
func TestAddClosesAnEntryThatHadNoComma(t *testing.T) {
	const file = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\"\n]\n"
	const want = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\",\n  \"9.9.9.9/32\",\n]\n"

	got, _, err := add(file, "ssh_allowed_cidrs", "9.9.9.9/32")
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestAddFillsAnEmptyList covers the first address going in.
func TestAddFillsAnEmptyList(t *testing.T) {
	got, changed, err := add("ssh_allowed_cidrs = []\n", "ssh_allowed_cidrs", "9.9.9.9/32")
	if err != nil {
		t.Fatal(err)
	}

	if !changed || got != "ssh_allowed_cidrs = [\"9.9.9.9/32\"]\n" {
		t.Fatalf("got %q", got)
	}
}

// TestAddSurvivesANonAsciiComment covers a comment holding a character of more than one byte,
// where blanking by rune shifts every later offset and slices an existing entry mid-value.
func TestAddSurvivesANonAsciiComment(t *testing.T) {
	const file = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\", # café\n  \"2.2.2.2/32\",\n]\n"
	const want = "ssh_allowed_cidrs = [\n  \"1.1.1.1/32\", # café\n  \"2.2.2.2/32\",\n  \"9.9.9.9/32\",\n]\n"

	got, changed, err := add(file, "ssh_allowed_cidrs", "9.9.9.9/32")
	if err != nil {
		t.Fatal(err)
	}

	if !changed || got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestWithoutCommentsKeepsEveryOffset covers the invariant the blanking exists for, which is
// that an index taken from the copy points at the same byte in the original.
func TestWithoutCommentsKeepsEveryOffset(t *testing.T) {
	for _, inner := range []string{
		"\n  \"1.1.1.1/32\", # café\n",
		"\n  \"1.1.1.1/32\", // naïve\n  \"2.2.2.2/32\",\n",
		"\"1.1.1.1/32\"",
	} {
		if got := len(withoutComments(inner)); got != len(inner) {
			t.Fatalf("blanking %q gave %d bytes, want %d", inner, got, len(inner))
		}
	}
}

// TestAddKeepsAnEmptyListOnItsOwnLines covers a list written open across lines with nothing in
// it yet, where filling it should not pull the closing bracket up.
func TestAddKeepsAnEmptyListOnItsOwnLines(t *testing.T) {
	got, _, err := add("ssh_allowed_cidrs = [\n]\n", "ssh_allowed_cidrs", "9.9.9.9/32")
	if err != nil {
		t.Fatal(err)
	}

	const want = "ssh_allowed_cidrs = [\n  \"9.9.9.9/32\",\n]\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
