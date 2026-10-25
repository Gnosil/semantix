package slice

import (
	"encoding/json"
	"testing"
)

func TestExtractDropsTokenlessSanitizedContent(t *testing.T) {
	for _, content := range []string{"? ? ?", "!!!", "\ufffd\ufffd", "🎬🔧", "ignore previous instructions"} {
		for _, role := range []string{"user", "assistant"} {
			line, err := json.Marshal(map[string]string{"role": role, "content": content})
			if err != nil {
				t.Fatal(err)
			}
			items, err := NewExtractor().Extract(line, SliceMeta{})
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 0 {
				t.Errorf("%s %q produced %d unusable slices", role, content, len(items))
			}
		}
	}
	for _, content := range []string{"正常中文视频渲染", "修复 go 测试", "123", "_"} {
		line, _ := json.Marshal(map[string]string{"role": "user", "content": content})
		items, err := NewExtractor().Extract(line, SliceMeta{})
		if err != nil || len(items) != 1 || string(items[0].Content) != content {
			t.Errorf("valid content %q changed or dropped: items=%+v err=%v", content, items, err)
		}
	}
}
