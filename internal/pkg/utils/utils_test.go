package utils

import "testing"

func TestReverse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		{name: "single char", input: "a", want: "a"},
		{name: "ascii", input: "hello", want: "olleh"},
		{name: "palindrome", input: "racecar", want: "racecar"},
		{name: "unicode", input: "한글", want: "글한"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reverse(tt.input)
			if got != tt.want {
				t.Errorf("Reverse(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReverseDoubleApply(t *testing.T) {
	cases := []string{"hello world", "한글 테스트", "🎉🚀"}
	for _, original := range cases {
		reversed := Reverse(original)
		doubleReversed := Reverse(reversed)
		if doubleReversed != original {
			t.Errorf("Reverse(Reverse(%q)) = %q, want %q", original, doubleReversed, original)
		}
	}
}
