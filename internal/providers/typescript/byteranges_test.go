package typescript_test

import "testing"

// TestByteRanges checks the syntax.Node byte-range extension (slice 1): a node
// spans a valid [start,end) range, and a child is contained within its parent's
// range. Containment is what pattern-inside relies on (slice 5).
func TestByteRanges(t *testing.T) {
	root := parseFile(t, "example.ts")
	fn := find(root, "function_declaration")
	if fn == nil {
		t.Fatal("no function_declaration")
	}
	if fn.StartByte() >= fn.EndByte() {
		t.Fatalf("invalid byte range: [%d,%d)", fn.StartByte(), fn.EndByte())
	}

	name := fn.ChildByField("name")
	if name == nil {
		t.Fatal("no name field")
	}
	// The name node must be contained within the function node by byte range.
	if name.StartByte() < fn.StartByte() || name.EndByte() > fn.EndByte() {
		t.Errorf("name [%d,%d) not contained in function [%d,%d)",
			name.StartByte(), name.EndByte(), fn.StartByte(), fn.EndByte())
	}
	// And it must be a strict sub-range (name is smaller than the whole function).
	if name.StartByte() == fn.StartByte() && name.EndByte() == fn.EndByte() {
		t.Error("name range equals function range; ranges look wrong")
	}
}
