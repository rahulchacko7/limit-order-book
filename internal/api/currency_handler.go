package api

import (
	"net/http"
	"strings"

	"limit-order-book/internal/models"
	"limit-order-book/internal/store"

	"github.com/gin-gonic/gin"
)

// CurrencyHandler provides HTTP handlers for currency management.
// API LAYER RESPONSIBILITIES:
// - Input validation
// - DTO to domain conversion
// - Delegate persistence to store layer
// - Return appropriate responses
type CurrencyHandler struct {
	currencyStore *store.CurrencyStore
}

// NewCurrencyHandler creates a new currency handler.
func NewCurrencyHandler(currencyStore *store.CurrencyStore) *CurrencyHandler {
	return &CurrencyHandler{
		currencyStore: currencyStore,
	}
}

// ListCurrencies handles GET /api/currencies
// Returns all active currencies (or all if include_inactive=true).
func (h *CurrencyHandler) ListCurrencies(c *gin.Context) {
	includeInactive := c.Query("include_inactive") == "true"

	currencies, err := h.currencyStore.List(c.Request.Context(), includeInactive)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list currencies"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"currencies": currencies,
		"count":      len(currencies),
	})
}

// GetCurrency handles GET /api/currencies/:code
// Returns a currency by its code.
func (h *CurrencyHandler) GetCurrency(c *gin.Context) {
	code := strings.ToUpper(c.Param("code"))
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency code is required"})
		return
	}

	currency, err := h.currencyStore.GetByCode(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get currency"})
		return
	}

	if currency == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "currency not found"})
		return
	}

	c.JSON(http.StatusOK, currency)
}

// CreateCurrency handles POST /api/currencies
// Creates a new currency.
func (h *CurrencyHandler) CreateCurrency(c *gin.Context) {
	var req CreateCurrencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate the request
	if err := h.validateCurrencyRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if currency already exists
	exists, err := h.currencyStore.Exists(c.Request.Context(), req.Code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check currency existence"})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "currency already exists"})
		return
	}

	// Create currency
	currency := &models.Currency{
		Code:      strings.ToUpper(req.Code),
		Name:      req.Name,
		Precision: req.Precision,
		MinAmount: req.MinAmount,
		IsActive:  true,
	}

	if err := h.currencyStore.Create(c.Request.Context(), currency); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create currency"})
		return
	}

	c.JSON(http.StatusCreated, currency)
}

// UpdateCurrency handles PUT /api/currencies/:code
// Updates an existing currency.
func (h *CurrencyHandler) UpdateCurrency(c *gin.Context) {
	code := strings.ToUpper(c.Param("code"))
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency code is required"})
		return
	}

	// Check if currency exists
	currency, err := h.currencyStore.GetByCode(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get currency"})
		return
	}
	if currency == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "currency not found"})
		return
	}

	var req UpdateCurrencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update fields if provided
	if req.Name != "" {
		currency.Name = req.Name
	}
	if req.Precision > 0 {
		currency.Precision = req.Precision
	}
	if req.MinAmount > 0 {
		currency.MinAmount = req.MinAmount
	}
	if req.IsActive != currency.IsActive {
		currency.IsActive = req.IsActive
	}

	if err := h.currencyStore.Update(c.Request.Context(), currency); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update currency"})
		return
	}

	c.JSON(http.StatusOK, currency)
}

// DeleteCurrency handles DELETE /api/currencies/:code
// Soft deletes a currency (sets is_active to false).
func (h *CurrencyHandler) DeleteCurrency(c *gin.Context) {
	code := strings.ToUpper(c.Param("code"))
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency code is required"})
		return
	}

	// Check if currency exists
	exists, err := h.currencyStore.Exists(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check currency"})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "currency not found"})
		return
	}

	if err := h.currencyStore.Delete(c.Request.Context(), code); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete currency"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "currency deleted successfully",
		"code":    code,
	})
}

// Request DTOs

// CreateCurrencyRequest is the request body for creating a currency.
type CreateCurrencyRequest struct {
	Code      string  `json:"code" binding:"required,len=3|len=4|len=5|len=10"`
	Name      string  `json:"name" binding:"required"`
	Precision int     `json:"precision" binding:"required,min=0,max=18"`
	MinAmount float64 `json:"min_amount" binding:"required,gte=0"`
}

// UpdateCurrencyRequest is the request body for updating a currency.
type UpdateCurrencyRequest struct {
	Name      string  `json:"name"`
	Precision int     `json:"precision"`
	MinAmount float64 `json:"min_amount"`
	IsActive  bool    `json:"is_active"`
}

// validateCurrencyRequest validates the currency request.
func (h *CurrencyHandler) validateCurrencyRequest(req *CreateCurrencyRequest) error {
	if len(req.Code) < 3 || len(req.Code) > 10 {
		return ErrInvalidCurrencyCode
	}
	if req.Precision < 0 || req.Precision > 18 {
		return ErrInvalidPrecision
	}
	if req.MinAmount < 0 {
		return ErrInvalidMinAmount
	}
	return nil
}

// Custom errors for validation.
var (
	ErrInvalidCurrencyCode = &ValidationError{Message: "currency code must be between 3 and 10 characters"}
	ErrInvalidPrecision    = &ValidationError{Message: "precision must be between 0 and 18"}
	ErrInvalidMinAmount    = &ValidationError{Message: "min_amount must be greater than or equal to 0"}
)
