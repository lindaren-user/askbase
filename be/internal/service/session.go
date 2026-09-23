package service

import (
	"context"
	"fmt"
	"strings"

	"askbase/be/internal/model"
	"askbase/be/internal/repo"
)

type sessionStore interface {
	Create(ctx context.Context, userID int64, datasetID int64, title string) (model.Session, error)
	GetByUser(ctx context.Context, userID int64, id int64) (model.Session, error)
	ListByUser(ctx context.Context, userID int64, datasetID *int64, status int16) ([]model.Session, error)
	ListArchivedByUser(ctx context.Context, userID int64) ([]model.Session, error)
	Update(ctx context.Context, userID int64, id int64, title *string, status *int16) error
	DeleteByUser(ctx context.Context, userID int64, id int64) error
	DeleteAllByUser(ctx context.Context, userID int64) error
}

type messageStore interface {
	ListBySession(ctx context.Context, sessionID int64) ([]model.Message, error)
	ListCitationsByMessageIDs(ctx context.Context, messageIDs []int64) ([]model.MessageCitation, error)
	DeleteBySession(ctx context.Context, sessionID int64) error
	DeleteAllByUser(ctx context.Context, userID int64) error
}

// SessionService 会话业务。
type SessionService struct {
	sessions  sessionStore
	messages  messageStore
	datasets  datasetByUserStore
	chunks    chunkLookupStore
	documents documentNameStore
	objects   objectURLSigner
}

type chunkLookupStore interface {
	ListByIDs(ctx context.Context, ids []int64) ([]model.Chunk, error)
}

type documentNameStore interface {
	ListNamesByIDs(ctx context.Context, ids []int64) (map[int64]string, error)
}

// NewSessionService 创建会话服务。
func NewSessionService(sessions sessionStore, messages messageStore, datasets datasetByUserStore, chunks chunkLookupStore, documents documentNameStore, objects objectURLSigner) *SessionService {
	return &SessionService{sessions: sessions, messages: messages, datasets: datasets, chunks: chunks, documents: documents, objects: objects}
}

// Create 新建会话；校验知识库归属。
func (s *SessionService) Create(ctx context.Context, userID int64, req model.CreateSessionRequest) (model.Session, error) {
	if _, err := s.datasets.GetByUser(ctx, userID, req.DatasetID); err != nil {
		return model.Session{}, asNotFound(err, repo.ErrDatasetNotFound)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "新会话"
	}
	return s.sessions.Create(ctx, userID, req.DatasetID, title)
}

// List 列出当前用户进行中的会话；可按知识库过滤。
func (s *SessionService) List(ctx context.Context, userID int64, datasetID *int64) ([]model.Session, error) {
	items, err := s.sessions.ListByUser(ctx, userID, datasetID, model.SessionStatusActive)
	if err != nil {
		return nil, err
	}
	markDatasetDeleted(items)
	return items, nil
}

// ListArchived 列出当前用户已归档会话（含知识库名）。
func (s *SessionService) ListArchived(ctx context.Context, userID int64) ([]model.Session, error) {
	items, err := s.sessions.ListArchivedByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	markDatasetDeleted(items)
	return items, nil
}

func markDatasetDeleted(items []model.Session) {
	for i := range items {
		items[i].DatasetDeleted = items[i].DatasetName == ""
	}
}

// Update 更新会话标题或归档状态。
func (s *SessionService) Update(ctx context.Context, userID int64, id int64, req model.UpdateSessionRequest) error {
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return fmt.Errorf("%w: 会话标题不能为空", ErrInvalidInput)
		}
		if len([]rune(title)) > 128 {
			return fmt.Errorf("%w: 会话标题过长", ErrInvalidInput)
		}
		req.Title = &title
	}
	status, err := parseSessionStatus(req.Status)
	if err != nil {
		return err
	}
	err = s.sessions.Update(ctx, userID, id, req.Title, status)
	return asNotFound(err, repo.ErrSessionNotFound)
}

func parseSessionStatus(raw *string) (*int16, error) {
	if raw == nil {
		return nil, nil
	}
	var status int16
	switch strings.TrimSpace(*raw) {
	case "active":
		status = model.SessionStatusActive
	case "archived":
		status = model.SessionStatusArchived
	default:
		return nil, fmt.Errorf("%w: 会话状态不正确", ErrInvalidInput)
	}
	return &status, nil
}

// Delete 删除会话及其消息。
func (s *SessionService) Delete(ctx context.Context, userID int64, id int64) error {
	if _, err := s.sessions.GetByUser(ctx, userID, id); err != nil {
		return asNotFound(err, repo.ErrSessionNotFound)
	}
	if err := s.messages.DeleteBySession(ctx, id); err != nil {
		return err
	}
	err := s.sessions.DeleteByUser(ctx, userID, id)
	return asNotFound(err, repo.ErrSessionNotFound)
}

// DeleteAllByUser 注销账号时清空该用户全部消息与会话。
func (s *SessionService) DeleteAllByUser(ctx context.Context, userID int64) error {
	if err := s.messages.DeleteAllByUser(ctx, userID); err != nil {
		return err
	}
	return s.sessions.DeleteAllByUser(ctx, userID)
}

// ListMessages 列出会话消息，并为 assistant 消息组装引用详情。
func (s *SessionService) ListMessages(ctx context.Context, userID int64, sessionID int64) ([]model.Message, error) {
	if _, err := s.sessions.GetByUser(ctx, userID, sessionID); err != nil {
		return nil, asNotFound(err, repo.ErrSessionNotFound)
	}
	messages, err := s.messages.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.attachCitations(ctx, messages)
}

// attachCitations 批量读取引用关系和分块，并还原生成回答时的编号与检索分数。
func (s *SessionService) attachCitations(ctx context.Context, messages []model.Message) ([]model.Message, error) {
	messageIDs := make([]int64, len(messages))
	for i := range messages {
		messageIDs[i] = messages[i].ID
	}
	references, err := s.messages.ListCitationsByMessageIDs(ctx, messageIDs)
	if err != nil {
		return nil, err
	}
	if len(references) == 0 {
		return messages, nil
	}
	idSet := make(map[int64]struct{}, len(references))
	byMessageID := make(map[int64][]model.MessageCitation)
	for _, reference := range references {
		idSet[reference.ChunkID] = struct{}{}
		byMessageID[reference.MessageID] = append(byMessageID[reference.MessageID], reference)
	}
	ids := make([]int64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	chunks, err := s.chunks.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]model.Chunk, len(chunks))
	docIDSet := map[int64]struct{}{}
	for _, chunk := range chunks {
		byID[chunk.ID] = chunk
		docIDSet[chunk.DocumentID] = struct{}{}
	}
	docIDs := make([]int64, 0, len(docIDSet))
	for id := range docIDSet {
		docIDs = append(docIDs, id)
	}
	docNames, err := s.documents.ListNamesByIDs(ctx, docIDs)
	if err != nil {
		return nil, err
	}
	for i := range messages {
		for _, reference := range byMessageID[messages[i].ID] {
			chunk, ok := byID[reference.ChunkID]
			if !ok {
				continue
			}
			messages[i].Citations = append(messages[i].Citations, model.Citation{
				N:            reference.Position,
				ChunkID:      chunk.ID,
				DocumentID:   chunk.DocumentID,
				DocumentName: docNames[chunk.DocumentID],
				Content:      chunk.Content,
				Score:        reference.Score,
				ImageURL:     presignImageURL(ctx, s.objects, chunk.ImageKey),
			})
		}
	}
	return messages, nil
}
