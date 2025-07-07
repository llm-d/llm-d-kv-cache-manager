package testdata

import (
	_ "embed"
)

const (
	ModelName = "bert-base-uncased"
)

//go:embed prompt.txt
var Prompt string

var PromptHashes = []uint64{
	17765219867688349152,
	10822023734066583577,
	15079747349478396262,
	6796279860526008575,
}
