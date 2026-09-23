package router

import (
	"net/http"

	"askbase/be/internal/handler"
	"askbase/be/internal/middleware"
	"askbase/be/internal/service"

	"github.com/go-chi/chi/v5"
)

// New 创建 HTTP 路由并挂载全局中间件。路由集中在此注册，按公共/认证分组。
func New(services service.Services, env string) http.Handler {
	h := handler.New(services)
	mux := chi.NewRouter()
	routes(mux, h, services, env)

	return middleware.Chain(
		mux,
		middleware.Recoverer,
		middleware.RequestID,
		middleware.CORS,
		middleware.RequestLogger,
	)
}

// routes 注册健康检查与 API 路由。
func routes(mux chi.Router, h *handler.Handler, services service.Services, env string) {
	mux.Get("/healthz", h.HandleHealth)

	mux.Route("/api/v1", func(api chi.Router) {
		// 公共组：无需登录
		api.Group(func(public chi.Router) {
			public.Route("/auth", func(auth chi.Router) {
				auth.Get("/turnstile-config", h.HandleTurnstileConfig)
				auth.Post("/send-code", h.HandleSendVerificationCode)
				auth.Post("/login", h.HandleLogin)
			})
		})

		// 认证组：session cookie 校验
		api.Group(func(protected chi.Router) {
			protected.Use(middleware.AuthRequired(services.Auth, h.WriteUnauthorized))
			protected.Get("/auth/me", h.HandleMe)
			protected.Post("/auth/logout", h.HandleLogout)
			protected.Delete("/auth/account", h.HandleDeleteAccount)

			protected.Route("/datasets", func(datasets chi.Router) {
				datasets.Get("/", h.HandleListDatasets)
				datasets.Post("/", h.HandleCreateDataset)
				datasets.Put("/{datasetId}", h.HandleUpdateDataset)
				datasets.Delete("/{datasetId}", h.HandleDeleteDataset)
				datasets.Get("/{datasetId}/documents", h.HandleListDocuments)
				datasets.Post("/{datasetId}/documents", h.HandleRegisterDocument)
			})

			protected.Route("/documents", func(documents chi.Router) {
				documents.Post("/upload-url", h.HandleCreateUploadURL)
				documents.Post("/batch-status", h.HandleBatchDocumentStatus)
				documents.Get("/{documentId}", h.HandleGetDocument)
				documents.Put("/{documentId}", h.HandleUpdateDocument)
				documents.Delete("/{documentId}", h.HandleDeleteDocument)
				documents.Post("/{documentId}/parse", h.HandleParseDocument)
				documents.Post("/{documentId}/stop", h.HandleStopDocument)
				documents.Get("/{documentId}/progress", h.HandleDocumentProgress)
				documents.Get("/{documentId}/preview", h.HandlePreviewDocument)
				documents.Get("/{documentId}/download", h.HandleDownloadDocument)
				documents.Get("/{documentId}/chunks", h.HandleListChunks)
			})

			protected.Put("/chunks/{chunkId}", h.HandleUpdateChunk)

			protected.Post("/retrieval/test", h.HandleRetrievalTest)

			protected.Route("/sessions", func(sessions chi.Router) {
				sessions.Get("/", h.HandleListSessions)
				sessions.Post("/", h.HandleCreateSession)
				sessions.Put("/{sessionId}", h.HandleUpdateSession)
				sessions.Delete("/{sessionId}", h.HandleDeleteSession)
				sessions.Get("/{sessionId}/messages", h.HandleListMessages)
			})

			protected.Post("/chat/completions", h.HandleChatCompletions)
			protected.Post("/models/test", h.HandleTestModel)
			protected.Post("/models/test-embed", h.HandleTestEmbed)
			protected.Post("/models/test-vision", h.HandleTestVision)

			protected.Route("/embed-models", func(embed chi.Router) {
				embed.Get("/", h.HandleListEmbedModels)
				embed.Post("/", h.HandleCreateEmbedModel)
				embed.Patch("/{embedModelId}", h.HandleUpdateEmbedModel)
				embed.Delete("/{embedModelId}", h.HandleDeleteEmbedModel)
			})
			protected.Route("/vision-models", func(vision chi.Router) {
				vision.Get("/", h.HandleListVisionModels)
				vision.Post("/", h.HandleCreateVisionModel)
				vision.Patch("/{visionModelId}", h.HandleUpdateVisionModel)
				vision.Delete("/{visionModelId}", h.HandleDeleteVisionModel)
			})
		})
	})
}
