package schemas

import _ "embed"

//go:embed structured-extraction-1.0.0.json
var structuredExtractionV1 []byte

//go:embed research-synthesis-1.0.0.json
var researchSynthesisV1 []byte

const StructuredExtractionVersion = "1.0.0"
const ResearchSynthesisVersion = "1.0.0"

func StructuredExtractionV1() []byte {
	return append([]byte(nil), structuredExtractionV1...)
}

func ResearchSynthesisV1() []byte {
	return append([]byte(nil), researchSynthesisV1...)
}
