package emoji

import (
	"errors"
	"strconv"
)

// parseDSRResponse parses a Device Status Report response of the form
// "\x1b[<row>;<col>R" and returns the column number (1-indexed).
// Returns an error if the response is malformed or column is < 1.
func parseDSRResponse(b []byte) (int, error) {
	// Must start with ESC [
	if len(b) < 4 || b[0] != 0x1B || b[1] != '[' {
		return 0, errors.New("missing CSI prefix")
	}
	// Must end with R
	if b[len(b)-1] != 'R' {
		return 0, errors.New("missing R terminator")
	}

	// Find semicolon
	semi := -1
	for i := 2; i < len(b)-1; i++ {
		if b[i] == ';' {
			semi = i
			break
		}
	}
	if semi == -1 {
		return 0, errors.New("missing semicolon")
	}

	// Parse column (between semi+1 and len-1)
	colStr := string(b[semi+1 : len(b)-1])
	col, err := strconv.Atoi(colStr)
	if err != nil {
		return 0, errors.New("invalid column: " + err.Error())
	}
	if col < 1 {
		return 0, errors.New("column must be >= 1")
	}
	return col, nil
}
