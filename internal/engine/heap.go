package engine

import "container/heap"

type OrderItem struct {
	order *OrderWrapper
	index int
}

type OrderHeap struct {
	items []*OrderItem
	isBuy bool
}

func (h OrderHeap) Len() int { return len(h.items) }

func (h OrderHeap) Less(i, j int) bool {
	oi := h.items[i].order
	oj := h.items[j].order

	if oi.Price == oj.Price {
		return oi.timestamp < oj.timestamp
	}

	if h.isBuy {
		return oi.Price > oj.Price
	}
	return oi.Price < oj.Price
}

func (h OrderHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
}

func (h *OrderHeap) Push(x interface{}) {
	h.items = append(h.items, x.(*OrderItem))
}

func (h *OrderHeap) Pop() interface{} {
	old := h.items
	n := len(old)
	item := old[n-1]
	h.items = old[:n-1]
	return item
}

func NewBuyHeap() *OrderHeap {
	h := &OrderHeap{isBuy: true}
	heap.Init(h)
	return h
}

func NewSellHeap() *OrderHeap {
	h := &OrderHeap{isBuy: false}
	heap.Init(h)
	return h
}
