package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"askbase/be/internal/model"
	"askbase/be/internal/queue"
)

// markFailed 将永久错误或重试耗尽原因可靠写入文档失败状态。
func (w *Worker) markFailed(ctx context.Context, msg queue.Message, cause error) error {
	message := strings.TrimPrefix(cause.Error(), errPermanent.Error()+": ")
	if _, err := w.documents.RecordParseAttempt(ctx, msg.DocumentID, msg.ParseVersion, msg.Attempt, time.Now(), message); err != nil {
		return fmt.Errorf("记录文档最终尝试失败: %w", err)
	}
	_, err := w.documents.UpdateStatusVersion(ctx, msg.DocumentID, msg.ParseVersion, model.DocumentStatusFailed, 0, message)
	if err != nil {
		return fmt.Errorf("更新文档失败状态出错: %w", err)
	}
	return nil
}

// markCancelled 保留当前进度并将文档落为已停止状态。
func (w *Worker) markCancelled(ctx context.Context, msg queue.Message) error {
	doc, err := w.documents.GetByID(ctx, msg.DocumentID)
	progress := int16(0)
	if err == nil {
		progress = doc.Progress
	}
	_, err = w.documents.UpdateStatusVersion(ctx, msg.DocumentID, msg.ParseVersion, model.DocumentStatusCancelled, progress, "已停止")
	if err != nil {
		return fmt.Errorf("更新文档停止状态出错: %w", err)
	}
	return nil
}

// setStatus 推进状态机；停止标记优先，不会被阶段推进覆盖。
func (w *Worker) setStatus(ctx context.Context, documentID int64, parseVersion int64, status string, progress int16) error {
	if err := w.checkCancelled(ctx, documentID); err != nil {
		return err
	}
	ok, err := w.documents.UpdateStatusVersionUnless(
		ctx,
		documentID,
		parseVersion,
		[]string{model.DocumentStatusCancelling, model.DocumentStatusCancelled},
		status,
		progress,
	)
	if err != nil {
		return fmt.Errorf("更新状态失败: %w", err)
	}
	if !ok {
		return errCancelled
	}
	return nil
}

// checkCancelled 同时检查调用上下文和数据库中的停止状态。
func (w *Worker) checkCancelled(ctx context.Context, documentID int64) error {
	if ctx.Err() != nil {
		return errCancelled
	}
	doc, err := w.documents.GetByID(ctx, documentID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return errCancelled
		}
		return fmt.Errorf("%w: 文档不存在: %v", errPermanent, err)
	}
	if isStopped(doc.Status) {
		return errCancelled
	}
	return nil
}

// watchCancel 轮询文档停止状态，并在用户停止时取消当前解析上下文。
// TODO: 后续改为阶段/批次边界检查持久化取消状态，避免每个运行任务持续高频查询数据库。
func (w *Worker) watchCancel(parent context.Context, documentID int64) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				doc, err := w.documents.GetByID(context.WithoutCancel(parent), documentID)
				if err == nil && isStopped(doc.Status) {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

// isStopped 判断文档是否处于停止中或已停止状态。
func isStopped(status string) bool {
	return status == model.DocumentStatusCancelling || status == model.DocumentStatusCancelled
}

// isCancelErr 统一识别内部停止标记和上下文取消错误。
func isCancelErr(err error) bool {
	return errors.Is(err, errCancelled) || errors.Is(err, context.Canceled)
}

// ioError 将下游上下文取消转换为流水线停止，其余错误保留原始原因。
func ioError(err error, message string) error {
	if errors.Is(err, context.Canceled) {
		return errCancelled
	}
	return fmt.Errorf("%s: %w", message, err)
}
