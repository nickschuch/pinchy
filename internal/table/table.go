package table

import (
	"bytes"
	"fmt"
	"io"

	"github.com/aquasecurity/table"
)

// Print renders a bordered table with rounded Unicode dividers to w.
func Print(w io.Writer, headers []string, rows [][]string) error {
	var b bytes.Buffer

	t := table.New(&b)

	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBrightBlack)
	t.SetDividers(table.UnicodeRoundedDividers)

	t.SetHeaders(headers...)

	for _, row := range rows {
		t.AddRow(row...)
	}

	t.Render()

	_, err := fmt.Fprintf(w, "\n%s\n", b.String())
	if err != nil {
		return fmt.Errorf("failed to print table: %w", err)
	}

	return nil
}
