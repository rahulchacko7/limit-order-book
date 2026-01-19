package api

import (
	"limit-order-book/internal/cache"
	"limit-order-book/internal/engine"
	"limit-order-book/internal/metrics"
	"limit-order-book/internal/middleware"
	"limit-order-book/internal/store"
	"limit-order-book/internal/ws"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine, ob *engine.OrderBookManager, cache *cache.RedisCache, wsHub *ws.Hub, currencyStore *store.CurrencyStore, pairStore *store.PairStore, m *metrics.Metrics) {
	authMiddleware := middleware.NewAuthMiddleware(middleware.DefaultAuthConfig())
	rateLimiter := middleware.NewRateLimiter(middleware.DefaultRateLimitConfig())

	r.Use(gin.Recovery())
	r.Use(middleware.RequestIDMiddleware())
	r.Use(middleware.LoggingMiddleware())

	h := NewHandler(ob, cache, wsHub)
	currencyHandler := NewCurrencyHandler(currencyStore)
	adminHandler := NewAdminHandler(ob, wsHub, cache, m)
	adminHandler.RegisterRoutes(r)

	api := r.Group("/api")
	{
		api.GET("/pairs", h.ListPairs)
		api.GET("/pairs/:pair/book", h.GetOrderBook)
		api.GET("/pairs/:pair/ticker", h.GetTicker)
		api.GET("/currencies", currencyHandler.ListCurrencies)
		api.GET("/currencies/:code", currencyHandler.GetCurrency)

		protected := api.Group("")
		protected.Use(authMiddleware.GinMiddleware())
		protected.Use(rateLimiter.GinMiddleware())
		{
			protected.POST("/orders", h.PlaceOrder)
			protected.GET("/orders", h.GetUserOrders)
			protected.GET("/orders/:id", h.GetOrder)
			protected.DELETE("/orders/:id", h.CancelOrder)
			protected.POST("/pairs", NewPairHandler(pairStore).CreatePair)
			protected.POST("/currencies", currencyHandler.CreateCurrency)
			protected.PUT("/currencies/:code", currencyHandler.UpdateCurrency)
			protected.DELETE("/currencies/:code", currencyHandler.DeleteCurrency)
		}
	}

	wsHandler := ws.NewHandler(wsHub)
	r.GET("/ws/:pair", wsHandler.HandleUpgrade)
	r.GET("/ws/stats", wsHandler.HandleStats)
}
