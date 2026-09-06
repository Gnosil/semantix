package slice

import "strings"

// Task type wire values produced by ClassifyTask. The set is closed and
// ordered by matching precedence (a text mentioning both a failing test and
// a fix classifies as test-update, not bugfix): more specific intents win.
const (
	TaskTestUpdate  = "test-update"
	TaskBugfix      = "bugfix"
	TaskRefactor    = "refactor"
	TaskDocs        = "docs"
	TaskFeature     = "feature"
	TaskInvestigate = "investigate"
	TaskGeneral     = "general"
)

// taskRules maps each task type to its trigger keywords (matched
// case-insensitively as substrings). Bilingual on purpose: the harness runs
// on English SWE tasks and on Chinese vertical workloads with the same
// kernel. Order of the outer slice is the precedence order.
var taskRules = []struct {
	kind string
	keys []string
}{
	{TaskTestUpdate, []string{
		"test fail", "failing test", "broken test", "update test", "fix test",
		"assertionerror", "测试失败", "修测试", "断言失败",
	}},
	{TaskBugfix, []string{
		"fix", "bug", "error", "crash", "regression", "incorrect", "wrong",
		"exception", "traceback", "unexpected", "fails", "broken",
		"修复", "报错", "崩溃", "异常", "错误", "不正确",
	}},
	{TaskRefactor, []string{
		"refactor", "rename", "clean up", "cleanup", "restructure", "simplify",
		"deduplicate", "重构", "重命名", "整理代码",
	}},
	{TaskDocs, []string{
		"document", "docs", "readme", "docstring", "changelog", "文档", "注释补全",
	}},
	{TaskFeature, []string{
		"add ", "support", "implement", "introduce", "new feature", "enable",
		"allow ", "新增", "支持", "实现", "增加",
	}},
	{TaskInvestigate, []string{
		"why ", "investigate", "diagnose", "figure out", "root cause",
		"what happens", "为什么", "排查", "诊断", "定位原因",
	}},
}

// ClassifyTask maps a task description (typically the turn's first user
// message) to one of the closed task-type values above. Deterministic and
// rule-driven — no model call: the tag gates plan-skeleton / outcome card
// admission (Injector.TaskType), so it must be reproducible offline. Empty
// or unmatched input classifies as TaskGeneral.
func ClassifyTask(text string) string {
	t := strings.ToLower(text)
	if strings.TrimSpace(t) == "" {
		return TaskGeneral
	}
	for _, rule := range taskRules {
		for _, key := range rule.keys {
			if containsTaskKey(t, key) {
				return rule.kind
			}
		}
	}
	return TaskGeneral
}

// containsTaskKey applies word boundaries to ASCII keyword edges while
// retaining substring matching for CJK keys. Without the boundary check,
// feature terms such as "prefix", "suffix", and "fixture" accidentally
// match the higher-precedence bugfix keyword "fix".
func containsTaskKey(text, key string) bool {
	for offset := 0; offset <= len(text)-len(key); {
		i := strings.Index(text[offset:], key)
		if i < 0 {
			return false
		}
		i += offset
		end := i + len(key)
		beforeOK := !asciiTaskWord(key[0]) || i == 0 || !asciiTaskWord(text[i-1])
		afterOK := !asciiTaskWord(key[len(key)-1]) || end == len(text) || !asciiTaskWord(text[end])
		if beforeOK && afterOK {
			return true
		}
		offset = i + 1
	}
	return false
}

func asciiTaskWord(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_'
}
