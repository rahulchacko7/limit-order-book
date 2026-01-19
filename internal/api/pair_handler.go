package api

import (
	"net/http"
	"strings"

	"limit-order-book/internal/models"
	"limit-order-book/internal/store"

	"github.com/gin-gonic/gin"
)

type PairHandler struct {
	pairStore *store.PairStore
}

func NewPairHandler(pairStore *store.PairStore) *PairHandler {
	return &PairHandler{pairStore: pairStore}
}

func (h *PairHandler) ListPairs(c *gin.Context) {
	pairs, err := h.pairStore.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list pairs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"pairs": pairs,
		"count": len(pairs),
	})
}

func (h *PairHandler) CreatePair(c *gin.Context) {
	var req CreatePairRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	base := strings.ToUpper(req.Base)
	quote := strings.ToUpper(req.Quote)

	exists, err := h.pairStore.Exists(c.Request.Context(), base, quote)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check pair existence"})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "pair already exists"})
		return
	}

	pair := &models.Pair{Base: base, Quote: quote}
	if err := h.pairStore.Create(c.Request.Context(), pair); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create pair"})
		return
	}

	c.JSON(http.StatusCreated, pair)
}

type CreatePairRequest struct {
	Base  string `json:"base" binding:"required"`
	Quote string `json:"quote" binding:"required"`
}
