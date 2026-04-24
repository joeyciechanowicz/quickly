package main

import "testing"

func bytes(parts ...string) []byte {
	out := make([]byte, 0)
	for _, part := range parts {
		out = append(out, []byte(part)...)
	}
	return out
}

func TestNormalisePassesThroughPlainText(t *testing.T) {
	lines, carry := normaliseChunk(bytes("hello", string([]byte{10}), "world", string([]byte{10})), "")
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Fatalf("got lines=%v carry=%q, want [hello world] carry=''", lines, carry)
	}
	if carry != "" {
		t.Fatalf("carry=%q, want ''", carry)
	}
}

func TestNormaliseCarryRetainsPartialLine(t *testing.T) {
	lines, carry := normaliseChunk([]byte("hel"), "")
	if len(lines) != 0 {
		t.Fatalf("got lines=%v, want []", lines)
	}
	if carry != "hel" {
		t.Fatalf("carry=%q, want 'hel'", carry)
	}
}

func TestNormaliseCRReplacesCurrentLine(t *testing.T) {
	lines, carry := normaliseChunk(bytes("loading...", string([]byte{13}), "Done", string([]byte{10})), "")
	if len(lines) != 1 || lines[0] != "Done" {
		t.Fatalf("got lines=%v carry=%q, want [Done] carry=''", lines, carry)
	}
}

func TestNormaliseCROnlyEmitsOnNewline(t *testing.T) {
	lines, carry := normaliseChunk(bytes("loading...", string([]byte{13}), "Done"), "")
	if len(lines) != 0 {
		t.Fatalf("got lines=%v, want []", lines)
	}
	if carry != "Done" {
		t.Fatalf("carry=%q, want 'Done'", carry)
	}
}

func TestNormaliseStripsEraseLine(t *testing.T) {
	esc := string([]byte{27})
	lines, carry := normaliseChunk([]byte("text"+esc+"[2K"), "")
	if len(lines) != 0 {
		t.Fatalf("got lines=%v, want []", lines)
	}
	if carry != "text" {
		t.Fatalf("carry=%q, want 'text'", carry)
	}
}

func TestNormaliseStripsCursorUp(t *testing.T) {
	esc := string([]byte{27})
	lines, carry := normaliseChunk([]byte("line"+esc+"[1Amore"+string([]byte{10})), "")
	if len(lines) != 1 || lines[0] != "linemore" {
		t.Fatalf("got lines=%v carry=%q, want [linemore]", lines, carry)
	}
}

func TestNormaliseKeepsColourCodes(t *testing.T) {
	esc := string([]byte{27})
	lines, carry := normaliseChunk([]byte(esc+"[32mgreen"+esc+"[0m"+string([]byte{10})), "")
	if len(lines) != 1 || lines[0] != "\033[32mgreen\033[0m" {
		t.Fatalf("got lines=%v carry=%q, want colour codes preserved", lines, carry)
	}
}

func TestNormaliseStripsOtherCursorMovement(t *testing.T) {
	esc := string([]byte{27})
	lines, carry := normaliseChunk([]byte(esc+"[H"+esc+"[2Jhello"+string([]byte{10})), "")
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("got lines=%v carry=%q, want [hello]", lines, carry)
	}
}

func TestNormaliseCarryPrependedToNextChunk(t *testing.T) {
	_, carry := normaliseChunk([]byte("hel"), "")
	lines, carry2 := normaliseChunk(bytes("lo", string([]byte{10})), carry)
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("got lines=%v carry=%q, want [hello]", lines, carry2)
	}
}

func TestNormaliseYarnStyleProgressLine(t *testing.T) {
	input := bytes("progress 10%", string([]byte{13}), "progress 20%", string([]byte{13}), "progress 100%", string([]byte{10}))
	lines, carry := normaliseChunk(input, "")
	if len(lines) != 1 || lines[0] != "progress 100%" {
		t.Fatalf("got lines=%v carry=%q, want [progress 100%%]", lines, carry)
	}
}
