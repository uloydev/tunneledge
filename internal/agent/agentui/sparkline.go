package agentui

import "strings"

// sparkBlocks are the eight Unicode block-element characters ordered from
// shortest to tallest, used to represent normalised bandwidth samples.
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// renderSparkline converts a slice of integer samples into a single-line
// Unicode sparkline string of at most maxCols characters.
//
// Samples are normalised relative to the maximum value in the slice so that
// the tallest bar always uses '█'. An empty or all-zero slice renders as a
// flat line of '▁' characters.
func renderSparkline(samples []int, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}

	// Take the last maxCols samples.
	data := samples
	if len(data) > maxCols {
		data = data[len(data)-maxCols:]
	}

	if len(data) == 0 {
		return strings.Repeat(string(sparkBlocks[0]), maxCols)
	}

	// Find peak for normalisation.
	peak := 1
	for _, v := range data {
		if v > peak {
			peak = v
		}
	}

	var sb strings.Builder
	for _, v := range data {
		// Map [0, peak] → [0, 7] index.
		idx := int(float64(v) / float64(peak) * float64(len(sparkBlocks)-1))
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		sb.WriteRune(sparkBlocks[idx])
	}

	// Pad with flat bars on the left when fewer samples than maxCols.
	pad := maxCols - len(data)
	if pad > 0 {
		return strings.Repeat(string(sparkBlocks[0]), pad) + sb.String()
	}
	return sb.String()
}
