// Probe existing session mirrors without sending model requests or printing content.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"semantix/kernel/slice"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: go run probe_local.go <session-root>")
	}
	total, skipped, contextCount, contextMatch, resultCount, resultMatch := 0, 0, 0, 0, 0, 0
	contextContained, resultContained, verifiedContained := 0, 0, 0
	verifiedResult, verifiedExact := 0, 0
	verificationFields, mutationFields := 0, 0
	err := filepath.WalkDir(os.Args[1], func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), "-deepseek-v4-flash.jsonl") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		total++
		var messages []string
		dec := json.NewDecoder(bytes.NewReader(data))
		for {
			var row struct {
				Role, Content string
				Verification  *string `json:"verification"`
				Mutation      *bool   `json:"workspace_mutation"`
			}
			if err := dec.Decode(&row); err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			if row.Role != "system" {
				messages = append(messages, row.Content)
			}
			if row.Verification != nil {
				verificationFields++
			}
			if row.Mutation != nil {
				mutationFields++
			}
		}
		items, err := slice.NewExtractor().Extract(data, slice.SliceMeta{})
		if err != nil {
			skipped++
			return nil
		}
		for _, item := range items {
			switch item.Type {
			case slice.Context:
				contextCount++
				if slices.Contains(messages, string(item.Content)) {
					contextMatch++
				}
				if containsMessage(messages, string(item.Content)) {
					contextContained++
				}
			case slice.Result:
				resultCount++
				if slices.Contains(messages, string(item.Content)) {
					resultMatch++
				}
				contained := containsMessage(messages, string(item.Content))
				if contained {
					resultContained++
				}
				if item.Meta.EffectiveResultStatus() == slice.ResultStatusVerified {
					verifiedResult++
					if slices.Contains(messages, string(item.Content)) {
						verifiedExact++
					}
					if contained {
						verifiedContained++
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	if total == 0 {
		panic("no matching session mirrors")
	}
	fmt.Printf("sessions=%d skipped=%d context=%d context_exact=%d context_contained=%d result=%d result_exact=%d result_contained=%d verified_result=%d verified_exact=%d verified_contained=%d verification_fields=%d mutation_fields=%d\n", total, skipped, contextCount, contextMatch, contextContained, resultCount, resultMatch, resultContained, verifiedResult, verifiedExact, verifiedContained, verificationFields, mutationFields)
}

func containsMessage(messages []string, content string) bool {
	if content == "" {
		return false
	}
	for _, message := range messages {
		if strings.Contains(message, content) {
			return true
		}
	}
	return false
}
