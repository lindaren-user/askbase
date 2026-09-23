package repo

import "errors"

// ErrUserNotFound 用户不存在或非正常状态。
var ErrUserNotFound = errors.New("user not found")

// ErrEmailExists 邮箱已被注册。
var ErrEmailExists = errors.New("email already exists")

// ErrDatasetNotFound 知识库不存在或不属于当前用户。
var ErrDatasetNotFound = errors.New("dataset not found")

// ErrDocumentNotFound 文档不存在或不属于当前用户。
var ErrDocumentNotFound = errors.New("document not found")

// ErrDocumentExists 相同内容文件已在知识库中登记（hash 幂等）。
var ErrDocumentExists = errors.New("document already exists")

// ErrChunkNotFound 分块不存在。
var ErrChunkNotFound = errors.New("chunk not found")

// ErrSessionNotFound 会话不存在或不属于当前用户。
var ErrSessionNotFound = errors.New("session not found")
