package engine

import "container/heap"

func (ob *OrderBook) matchBuy(buy *OrderWrapper) {
	for buy.Remaining() > 0 && ob.sellHeap.Len() > 0 {
		bestSell := ob.sellHeap.items[0].order

		if buy.Price < bestSell.Price {
			break
		}

		ob.executeTrade(buy, bestSell)

		if bestSell.Remaining() == 0 {
			heap.Pop(ob.sellHeap)
			delete(ob.ordersByID, bestSell.ID)
		}
	}
}

func (ob *OrderBook) matchSell(sell *OrderWrapper) {
	for sell.Remaining() > 0 && ob.buyHeap.Len() > 0 {
		bestBuy := ob.buyHeap.items[0].order

		if sell.Price > bestBuy.Price {
			break
		}

		ob.executeTrade(bestBuy, sell)

		if bestBuy.Remaining() == 0 {
			heap.Pop(ob.buyHeap)
			delete(ob.ordersByID, bestBuy.ID)
		}
	}
}

func (ob *OrderBook) executeTrade(buy, sell *OrderWrapper) {
	qty := min(buy.Remaining(), sell.Remaining())
	buy.Fill(qty)
	sell.Fill(qty)

	trade := &TradeResult{
		BuyOrderID:  buy.ID,
		SellOrderID: sell.ID,
		Price:       sell.Price,
		Quantity:    qty,
		BuyFilled:   buy.Filled,
		SellFilled:  sell.Filled,
	}

	if ob.onTrade != nil {
		ob.onTrade(buy.Pair, trade)
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
