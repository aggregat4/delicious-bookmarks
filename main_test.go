package main

import (
	"testing"
)

func TestHighlight(t *testing.T) {
	// Test that the function correctly replaces "{{mark}}" and "{{endmark}}" with HTML <mark> tags
	input := "{{mark}}Hello, world!{{endmark}}"
	expected := "<mark>Hello, world!</mark>"
	if output := highlight(input); output != expected {
		t.Errorf("highlight(%q) returned %q, expected %q", input, output, expected)
	}

	// Test that the function correctly handles input that does not contain "{{mark}}" and "{{endmark}}"
	input = "Hello, world!"
	expected = "Hello, world!"
	if output := highlight(input); output != expected {
		t.Errorf("highlight(%q) returned %q, expected %q", input, output, expected)
	}

	// Test that the function correctly handles input that contains "{{mark}}" and "{{endmark}}" with no text in between
	input = "{{mark}}{{endmark}}"
	expected = "<mark></mark>"
	if output := highlight(input); output != expected {
		t.Errorf("highlight(%q) returned %q, expected %q", input, output, expected)
	}
}
