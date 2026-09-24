package slice

import "testing"

func TestTaskBodyPreservesUnicodeAndUnrelatedTags(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"<execution-policy>İ Fix tests</execution-policy><issue>修复 İ parser</issue>", "修复 İ parser"},
		{"İ preface <ISSUE>修复 parser</ISSUE>", "修复 parser"},
		{"Fix <execution-policy-example> markup", "Fix <execution-policy-example> markup"},
		{"<execution-policy-example>keep</execution-policy-example><execution-policy mode=\"strict\">remove</execution-policy>\nIssue:\nFix parser", "<execution-policy-example>keep</execution-policy-example> \nIssue:\nFix parser"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := TaskBody(tc.input); got != tc.want {
				t.Fatalf("TaskBody = %q, want %q", got, tc.want)
			}
		})
	}
}
