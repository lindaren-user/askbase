// Package worker 文档解析流水线消费者：parse → chunk → embed → upsert。
// 消费 queue 包投递的任务，at-least-once；每步幂等，失败重投，超限置 failed。
package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"askbase/be/internal/config"
	"askbase/be/internal/llm"
	"askbase/be/internal/model"
	"askbase/be/internal/queue"
	documentparser "askbase/be/internal/worker/parser"

	"go.uber.org/zap"
)

// errPermanent 标记永久性错误（如不支持的文件类型），由消费层落终态后转入 DLQ。
var errPermanent = documentparser.ErrPermanent

// errCancelled 标记用户主动停止解析：ACK 消息，不再重投。
var errCancelled = errors.New("parse cancelled")

// documentStore 提供流水线所需的文档读取与状态机操作。
type documentStore interface {
	GetByID(ctx context.Context, id int64) (model.Document, error)
	RecordParseAttempt(ctx context.Context, id int64, parseVersion int64, attempt int, attemptedAt time.Time, lastError string) (bool, error)
	UpdateStatusVersion(ctx context.Context, id int64, parseVersion int64, status string, progress int16, parseError string) (bool, error)
	UpdateStatusVersionUnless(ctx context.Context, id int64, parseVersion int64, excludedStatuses []string, status string, progress int16) (bool, error)
}

// datasetLookup 读取文档所属知识库及其解析配置。
type datasetLookup interface {
	GetByID(ctx context.Context, id int64) (model.Dataset, error)
}

// chunkStore 持久化分块、图片引用和嵌入向量。
type chunkStore interface {
	BulkInsert(ctx context.Context, chunks []model.Chunk) error
	DeleteByDocument(ctx context.Context, documentID int64) error
	ListImageKeysByDocument(ctx context.Context, documentID int64) ([]string, error)
	UpdateVector(ctx context.Context, id int64, values []float32) error
}

// embedModelLookup 读取知识库绑定的嵌入模型。
type embedModelLookup interface {
	GetByID(ctx context.Context, id int64) (model.EmbedModel, error)
}

// visionModelLookup 读取知识库绑定的视觉模型。
type visionModelLookup interface {
	GetByID(ctx context.Context, id int64) (model.VisionModel, error)
}

// embedder 提供批量文本向量化能力。
type embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// imageDescriber 提供图片转录与理解能力。
type imageDescriber interface {
	DescribeImage(ctx context.Context, mime string, image []byte, prompt string) (string, error)
}

// textCompleter 提供非流式文本生成，用于需要完整结构化结果的解析任务。
type textCompleter interface {
	Complete(ctx context.Context, messages []llm.ChatMessage, maxTokens int) (string, error)
}

// objectStore 提供解析流水线实际使用的对象读写能力。
type objectStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

// Worker 解析流水线消费者。
type Worker struct {
	documents      documentStore
	datasets       datasetLookup
	embedModels    embedModelLookup
	visionModels   visionModelLookup
	chunks         chunkStore
	objects        objectStore
	batchSize      int
	officialEmbed  embedder
	officialVision imageDescriber
	resumeLLM      textCompleter
	mineru         *minerUClient
	locker         documentLocker
}

// New 创建流水线消费者。
func New(documents documentStore, datasets datasetLookup, embedModels embedModelLookup, visionModels visionModelLookup, chunks chunkStore, objects objectStore, batchSize int, officialEmbed embedder, officialVision imageDescriber, minerUConfig config.MinerUConfig, locker documentLocker) *Worker {
	if batchSize <= 0 {
		batchSize = 32
	}
	worker := &Worker{
		documents:      documents,
		datasets:       datasets,
		embedModels:    embedModels,
		visionModels:   visionModels,
		chunks:         chunks,
		objects:        objects,
		batchSize:      batchSize,
		officialEmbed:  officialEmbed,
		officialVision: officialVision,
		mineru:         newMinerUClient(minerUConfig),
		locker:         locker,
	}
	if completer, ok := officialEmbed.(textCompleter); ok {
		worker.resumeLLM = completer
	}
	return worker
}

// HandleParse 处理一条解析任务；返回错误时由 RabbitMQ 消费层决定重试或终态处理。
func (w *Worker) HandleParse(ctx context.Context, msg queue.Message) error {
	log := zap.L().With(zap.Int64("documentId", msg.DocumentID), zap.Int("attempt", msg.Attempt))
	if w.locker != nil {
		release, acquired, err := w.locker.TryLock(ctx, msg.DocumentID)
		if err != nil {
			return err
		}
		if !acquired {
			return queue.RetryWithoutPenalty(fmt.Errorf("文档正在由其他 worker 处理"))
		}
		defer release()
	}
	doc, err := w.documents.GetByID(ctx, msg.DocumentID)
	if err != nil {
		return queue.Permanent(fmt.Errorf("文档不存在: %w", err))
	}
	if doc.ParseVersion != msg.ParseVersion || doc.Status == model.DocumentStatusDone {
		return nil
	}
	if ok, err := w.documents.RecordParseAttempt(ctx, msg.DocumentID, msg.ParseVersion, msg.Attempt, time.Now(), ""); err != nil {
		return err
	} else if !ok {
		return nil
	}

	parentCtx := ctx
	ctx, cancelWatch := w.watchCancel(ctx, msg.DocumentID)
	defer cancelWatch()
	err = w.runPipeline(ctx, msg.DocumentID, msg.ParseVersion, log)
	if err == nil {
		return nil
	}
	if parentCtx.Err() != nil {
		return parentCtx.Err()
	}
	if isCancelErr(err) {
		log.Info("文档解析已停止")
		return w.markCancelled(parentCtx, msg)
	}
	if errors.Is(err, errPermanent) {
		log.Warn("文档解析永久失败", zap.Error(err))
		return queue.Permanent(err)
	}
	if _, recordErr := w.documents.RecordParseAttempt(parentCtx, msg.DocumentID, msg.ParseVersion, msg.Attempt, time.Now(), err.Error()); recordErr != nil {
		return recordErr
	}
	log.Warn("文档解析失败，等待重投", zap.Error(err))
	return err
}

// HandleTerminal 可靠记录永久失败或重试耗尽的文档终态。
func (w *Worker) HandleTerminal(ctx context.Context, msg queue.Message, cause error) error {
	return w.markFailed(ctx, msg, cause)
}

// runPipeline 执行完整流水线。每步幂等：重投时从头执行，先清旧分块再重建。
// 嵌入模型取知识库绑定，向量按实测长度写入 q_N_vec。
func (w *Worker) runPipeline(ctx context.Context, documentID int64, parseVersion int64, log *zap.Logger) error {
	doc, err := w.documents.GetByID(ctx, documentID)
	if err != nil {
		return fmt.Errorf("%w: 文档不存在: %v", errPermanent, err)
	}
	if doc.Status == model.DocumentStatusDone {
		return nil
	}
	if err := w.checkCancelled(ctx, documentID); err != nil {
		return err
	}
	dataset, err := w.datasets.GetByID(ctx, doc.DatasetID)
	if err != nil {
		return fmt.Errorf("%w: 知识库不存在: %v", errPermanent, err)
	}
	emb, err := w.resolveEmbedder(ctx, dataset)
	if err != nil {
		return err
	}
	vision, err := w.resolveVision(ctx, dataset)
	if err != nil {
		return err
	}
	strategy := dataset.ChunkStrategy
	if !model.IsChunkStrategy(strategy) {
		return fmt.Errorf("%w: 知识库解析模板无效: %q", errPermanent, strategy)
	}

	// 流水线进度：0% 排队，10% 开始下载并解析，40% 开始分块，60% 开始向量化，
	// 60%～95% 按实际完成的向量批次数推进，100% 表示全部完成。
	// 1. 解析：从对象存储拉回并拆成段落、表格和图片原子。
	if err := w.setStatus(ctx, documentID, parseVersion, model.DocumentStatusParsing, 10); err != nil {
		return err
	}
	atoms, err := w.fetchAndParse(ctx, doc, strategy)
	if err != nil {
		return err
	}
	if len(atoms) == 0 {
		return fmt.Errorf("%w: 文档内容为空", errPermanent)
	}

	// 2. 分块：清旧块及旧图后按原子顺序重建。
	if err := w.setStatus(ctx, documentID, parseVersion, model.DocumentStatusChunking, 40); err != nil {
		return err
	}
	if oldKeys, err := w.chunks.ListImageKeysByDocument(ctx, documentID); err == nil {
		for _, key := range oldKeys {
			_ = w.objects.Delete(ctx, key)
		}
	}
	if err := w.chunks.DeleteByDocument(ctx, documentID); err != nil {
		return err
	}
	chunks, embedTexts, err := w.materializeChunks(ctx, doc, atoms, strategy, vision)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return fmt.Errorf("%w: 分块结果为空", errPermanent)
	}
	if err := w.chunks.BulkInsert(ctx, chunks); err != nil {
		return err
	}
	chunkIDs := make([]int64, len(chunks))
	for index := range chunks {
		chunkIDs[index] = chunks[index].ID
	}
	log.Info("分块完成", zap.Int("chunks", len(chunks)), zap.String("strategy", strategy))

	// 3. 向量化：批量嵌入并回填。
	if err := w.setStatus(ctx, documentID, parseVersion, model.DocumentStatusEmbedding, 60); err != nil {
		return err
	}
	if err := w.embedChunks(ctx, emb, chunkIDs, embedTexts, documentID, parseVersion); err != nil {
		return err
	}

	// 4. 完成。
	if err := w.setStatus(ctx, documentID, parseVersion, model.DocumentStatusDone, 100); err != nil {
		return err
	}
	log.Info("文档解析完成", zap.Int("chunks", len(chunks)))
	return nil
}
