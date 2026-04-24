package main

// normaliseChunk processes a raw PTY output chunk into complete lines.
// carry is any incomplete line from a previous chunk; it is prepended to
// the first token in p. Returns completed lines and the new carry.
//
// Rules:
//   - newline emits current line and resets carry
//   - carriage return resets the current line for progress-bar rewrites
//   - CSI color/style sequences ending in 'm' are kept
//   - other CSI cursor movement / erase / clear sequences are discarded
//   - other escape sequences are discarded
func normaliseChunk(p []byte, carry string) (lines []string, newCarry string) {
	cur := carry
	i := 0
	for i < len(p) {
		b := p[i]
		switch {
		case b == 10:
			lines = append(lines, cur)
			cur = ""
			i++
		case b == 13:
			// \r\n is a normal PTY line terminator — treat as just \n.
			if i+1 < len(p) && p[i+1] == 10 {
				lines = append(lines, cur)
				cur = ""
				i += 2
				continue
			}
			cur = ""
			i++
		case b == 27 && i+1 < len(p) && p[i+1] == '[':
			seq, n := parseCSI(p, i)
			final := seq[len(seq)-1]
			if final == 'm' {
				cur += string(seq)
			}
			i += n
		case b == 27:
			i += 2
			if i > len(p) {
				i = len(p)
			}
		default:
			cur += string(b)
			i++
		}
	}
	return lines, cur
}

// parseCSI parses a CSI sequence starting at p[start].
func parseCSI(p []byte, start int) (seq []byte, n int) {
	if start+1 >= len(p) {
		return p[start : start+1], 1
	}
	j := start + 2
	for j < len(p) {
		c := p[j]
		j++
		if c >= 0x40 && c <= 0x7E {
			return p[start:j], j - start
		}
	}
	return p[start : start+1], 1
}
