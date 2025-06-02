package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type PrefixedWriter struct {
	directory string
	writer    io.Writer
	color     string
}

func (w *PrefixedWriter) Write(p []byte) (n int, err error) {
	scanner := bufio.NewScanner(strings.NewReader(string(p)))
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(w.writer, "%s[%s]%s %s\n",
			w.color,
			w.directory,
			resetColor,
			line,
		)
	}

	return len(p), nil
}
