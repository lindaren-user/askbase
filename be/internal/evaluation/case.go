package evaluation

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"askbase/be/internal/model"
	"askbase/be/internal/service"
)

// Evidence 描述一个稳定的相关证据，由文档名和必须同时出现的文本片段定位。
type Evidence struct {
	DocumentName string   `json:"documentName"`
	Contains     []string `json:"contains"`
}

// Case 是一条路由与检索黄金测试用例。
type Case struct {
	ID               string            `json:"id"`
	DatasetID        int64             `json:"datasetId"`
	History          []model.Message   `json:"history"`
	Question         string            `json:"question"`
	ExpectedRoute    service.QueryType `json:"expectedRoute"`
	RelevantEvidence []Evidence        `json:"relevantEvidence"`
	ExpectNoEvidence bool              `json:"expectNoEvidence,omitempty"`
}

// LoadCases 从 JSONL 文件逐行读取并校验评测用例。
func LoadCases(path string) ([]Case, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开评测用例失败: %w", err)
	}
	defer file.Close()
	return DecodeCases(file)
}

// DecodeCases 从输入流读取 JSONL 用例，空行会被忽略。
func DecodeCases(reader io.Reader) ([]Case, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	cases := make([]Case, 0)
	seen := make(map[string]struct{})
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item Case
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("第 %d 行不是有效 JSON: %w", lineNumber, err)
		}
		if err := validateCase(item); err != nil {
			return nil, fmt.Errorf("第 %d 行用例无效: %w", lineNumber, err)
		}
		if _, ok := seen[item.ID]; ok {
			return nil, fmt.Errorf("第 %d 行用例 ID 重复: %s", lineNumber, item.ID)
		}
		seen[item.ID] = struct{}{}
		cases = append(cases, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取评测用例失败: %w", err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("评测用例不能为空")
	}
	return cases, nil
}

func validateCase(item Case) error {
	if strings.TrimSpace(item.ID) == "" {
		return fmt.Errorf("id 不能为空")
	}
	if strings.TrimSpace(item.Question) == "" {
		return fmt.Errorf("question 不能为空")
	}
	if !validQueryType(item.ExpectedRoute) {
		return fmt.Errorf("expectedRoute %q 无效", item.ExpectedRoute)
	}
	if item.ExpectedRoute != service.QueryTypeNone && item.DatasetID <= 0 {
		return fmt.Errorf("需要检索的用例必须提供 datasetId")
	}
	if item.ExpectedRoute != service.QueryTypeNone && len(item.RelevantEvidence) == 0 && !item.ExpectNoEvidence {
		return fmt.Errorf("需要检索的用例必须提供 relevantEvidence 或设置 expectNoEvidence")
	}
	if len(item.RelevantEvidence) > 0 && item.ExpectNoEvidence {
		return fmt.Errorf("relevantEvidence 与 expectNoEvidence 不能同时设置")
	}
	for i, evidence := range item.RelevantEvidence {
		if strings.TrimSpace(evidence.DocumentName) == "" {
			return fmt.Errorf("第 %d 条证据缺少 documentName", i+1)
		}
		if len(evidence.Contains) == 0 {
			return fmt.Errorf("第 %d 条证据缺少 contains", i+1)
		}
		for _, marker := range evidence.Contains {
			if strings.TrimSpace(marker) == "" {
				return fmt.Errorf("第 %d 条证据包含空文本标记", i+1)
			}
		}
	}
	for i, message := range item.History {
		if message.Role != model.MessageRoleUser && message.Role != model.MessageRoleAssistant {
			return fmt.Errorf("第 %d 条历史消息角色无效", i+1)
		}
		if strings.TrimSpace(message.Content) == "" {
			return fmt.Errorf("第 %d 条历史消息内容为空", i+1)
		}
	}
	return nil
}

func validQueryType(queryType service.QueryType) bool {
	switch queryType {
	case service.QueryTypeNone,
		service.QueryTypeDirect,
		service.QueryTypeContextualRewrite,
		service.QueryTypeDecomposition:
		return true
	default:
		return false
	}
}
