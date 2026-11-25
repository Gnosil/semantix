package slice

import (
	"sort"
	"strings"

	"semantix/kernel/graph"
)

// DreamObservations replays host tool results from one Semantix transcript.
// A file observation needs both a structured tool argument and a host-reported
// mutation. A verification relation describes sequence, not file coverage.
func DreamObservations(data []byte) ([]graph.Triple, error) {
	lines, err := parseTranscript(data)
	if err != nil {
		return nil, err
	}
	calls := make(map[string]toolCall)
	seenResults := make(map[string]bool)
	edited := make(map[string]struct{})
	verifiedCommands := make(map[string]bool)
	for _, line := range lines {
		for _, call := range line.ToolCalls {
			if call.ID != "" {
				calls[call.ID] = toolCall{name: call.Name, args: decodeToolArgs(call.Arguments)}
			}
		}
		if line.Role != "tool" || line.ToolCall == "" || seenResults[line.ToolCall] {
			continue
		}
		seenResults[line.ToolCall] = true
		call := calls[line.ToolCall]
		if line.Verification == "passed" && shellToolNames[strings.ToLower(call.name)] {
			verifiedCommands[commandValue(call.args)] = true
		}
		if !line.WorkspaceMutation {
			continue
		}
		switch strings.ToLower(call.name) {
		case "editfile", "edit_file", "writefile", "write_file", "multiedit", "multi_edit":
			args, _ := call.args.(map[string]any)
			for _, key := range []string{"path", "file_path", "file"} {
				raw, _ := args[key].(string)
				if path, ok := normalizeContextPath(raw); ok {
					edited[path] = struct{}{}
				}
			}
		}
	}
	files := make([]string, 0, len(edited))
	for file := range edited {
		files = append(files, file)
	}
	sort.Strings(files)
	command, verified := resultVerifiedAfterLatestMutation(lines)
	var out []graph.Triple
	for i, file := range files {
		if verified && verifiedCommands[command] {
			out = append(out, graph.Triple{Relation: "verifies", From: normalizeCommand(command), To: file})
		}
		for _, other := range files[i+1:] {
			out = append(out, graph.Triple{Relation: "co_edited", From: file, To: other})
		}
	}
	return out, nil
}
