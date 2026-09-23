package model

import (
	"time"
)

// 消息角色。
const (
	MessageRoleUser      = "user"
	MessageRoleAssistant = "assistant"
)

// Message 对应 t_message。
type Message struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	SessionID int64     `json:"sessionId"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`

	// 引用详情由消息引用关系和分块共同组装，不直接映射消息表。
	Citations []Citation `json:"citations,omitempty" gorm:"-"`
}

// TableName 指定消息表名。
func (Message) TableName() string {
	return "t_message"
}

// MessageCitation 保存一条消息引用的分块、编号和检索时分数。
type MessageCitation struct {
	MessageID int64   `json:"-" gorm:"primaryKey"`
	Position  int     `json:"-" gorm:"primaryKey"`
	ChunkID   int64   `json:"-"`
	Score     float64 `json:"-"`
}

// TableName 指定消息引用表名。
func (MessageCitation) TableName() string {
	return "t_message_citation"
}

// Citation 引用溯源：答案 [n] 对应的分块原文。
type Citation struct {
	N            int     `json:"n"`
	ChunkID      int64   `json:"chunkId,string"`
	DocumentID   int64   `json:"documentId"`
	DocumentName string  `json:"documentName"`
	Content      string  `json:"content"`
	Score        float64 `json:"score"`
	ImageURL     string  `json:"imageUrl,omitempty"`
}

// ChatCompletionRequest SSE 对话请求。
type ChatCompletionRequest struct {
	SessionID int64            `json:"sessionId"`
	Content   string           `json:"content"`
	Model     *ChatModelChoice `json:"model,omitempty"` // 可选：自定义对话模型覆盖（BYOK）
}
