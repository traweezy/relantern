package release

import "testing"

func TestValidateSourceAcceptsSameRepositoryStagingPR(t *testing.T) {
	t.Parallel()
	err := ValidateSource(SourceInput{
		EventName: "pull_request", BaseRef: "master", HeadRef: "staging",
		BaseRepository: "traweezy/relantern", HeadRepository: "traweezy/relantern",
	})
	if err != nil {
		t.Fatalf("ValidateSource() error = %v", err)
	}
}

func TestValidateSourceFailsClosed(t *testing.T) {
	t.Parallel()
	valid := SourceInput{
		EventName: "pull_request", BaseRef: "master", HeadRef: "staging",
		BaseRepository: "traweezy/relantern", HeadRepository: "traweezy/relantern",
	}
	tests := []struct {
		name   string
		mutate func(*SourceInput)
	}{
		{name: "wrong event", mutate: func(input *SourceInput) { input.EventName = "push" }},
		{name: "wrong refs", mutate: func(input *SourceInput) { input.HeadRef = "feature/release" }},
		{name: "fork", mutate: func(input *SourceInput) { input.HeadRepository = "attacker/relantern" }},
		{name: "draft", mutate: func(input *SourceInput) { input.Draft = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if err := ValidateSource(input); err == nil {
				t.Fatal("ValidateSource() accepted an invalid release source")
			}
		})
	}
}
