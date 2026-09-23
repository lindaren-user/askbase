package service

import (
	"context"
	"fmt"
	"strings"
)

// QueryPlan 是 Chat 和评测器共同使用的最终查询计划。
type QueryPlan struct {
	QueryType QueryType `json:"queryType"`
	Queries   []string  `json:"queries"`
	Fallback  bool      `json:"fallback"`
}

// QueryPlanner 将路由结果转换为可直接执行的查询计划。
// 路由失败时仍返回 direct 兜底计划，并同时返回原始错误供调用方记录。
type QueryPlanner interface {
	Plan(ctx context.Context, input QueryRouteInput) (QueryPlan, error)
}

// DefaultQueryPlanner 统一实现线上 Chat 与离线评测使用的路由兜底规则。
type DefaultQueryPlanner struct {
	router        QueryRouter
	maxSubqueries int
}

// NewQueryPlanner 创建默认查询计划器。
func NewQueryPlanner(router QueryRouter, maxSubqueries int) *DefaultQueryPlanner {
	return &DefaultQueryPlanner{
		router:        router,
		maxSubqueries: normalizeMaxSubqueries(maxSubqueries),
	}
}

// Plan 生成查询计划；模型错误或非法输出统一回退到原问题直接检索。
func (p *DefaultQueryPlanner) Plan(
	ctx context.Context,
	input QueryRouteInput,
) (QueryPlan, error) {
	question := strings.TrimSpace(input.Question)
	if question == "" {
		return QueryPlan{}, fmt.Errorf("问题不能为空")
	}
	fallback := QueryPlan{
		QueryType: QueryTypeDirect,
		Queries:   []string{question},
		Fallback:  true,
	}
	if p.router == nil {
		return fallback, fmt.Errorf("查询路由器未配置")
	}

	decision, err := p.router.Route(ctx, input)
	if err != nil {
		return fallback, err
	}
	decision, err = normalizeRouteDecision(decision, question, p.maxSubqueries)
	if err != nil {
		return fallback, err
	}
	return QueryPlan{
		QueryType: decision.QueryType,
		Queries:   buildQueryPlan(question, decision, p.maxSubqueries),
		Fallback:  false,
	}, nil
}
