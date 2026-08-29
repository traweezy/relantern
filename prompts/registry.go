package prompts

import _ "embed"

//go:embed structured-extraction-1.0.0.txt
var structuredExtractionV1 string

//go:embed research-synthesis-1.0.0.txt
var researchSynthesisV1 string

const StructuredExtractionVersion = "1.0.0"
const ResearchSynthesisVersion = "1.0.0"

func StructuredExtractionV1() string {
	return structuredExtractionV1
}

func ResearchSynthesisV1() string {
	return researchSynthesisV1
}
