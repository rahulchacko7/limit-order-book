package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"limit-order-book/internal/api"
	"limit-order-book/internal/cache"
	"limit-order-book/internal/config"
	"limit-order-book/internal/engine"
	"limit-order-book/internal/messaging"
	"limit-order-book/internal/metrics"
	"limit-order-book/internal/models"
	"limit-order-book/internal/store"
	"limit-order-book/internal/ws"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()
	obManager := engine.NewOrderBookManager()

	var redisCache *cache.RedisCache
	redisCache, err := cache.NewRedisCache(cfg)
	if err != nil {
		log.Printf("⚠️ Redis cache not available: %v", err)
		redisCache = nil
	} else {
		log.Println("✅ Redis cache connected")
	}
	defer func() {
		if redisCache != nil {
			redisCache.Close()
		}
	}()

	wsHub := ws.NewHub()
	go wsHub.Run()
	log.Println("✅ WebSocket hub started")

	var postgresStore *store.PostgresStore
	postgresStore, err = store.NewPostgresStore(cfg.GetPostgresDSN())
	if err != nil {
		log.Printf("⚠️ PostgreSQL store not available: %v", err)
		postgresStore = nil
	} else {
		log.Println("✅ PostgreSQL store connected")
	}
	defer func() {
		if postgresStore != nil {
			postgresStore.Close()
		}
	}()

	var currencyStore *store.CurrencyStore
	var pairStore *store.PairStore
	if postgresStore != nil {
		currencyStore = store.NewCurrencyStore(postgresStore.GetDB())

		seeded, err := currencyStore.SeedDefaultCurrencies(context.Background(), models.DefaultCurrencies)
		if err != nil {
			log.Printf("⚠️ Failed to seed default currencies: %v", err)
		} else if seeded > 0 {
			log.Printf("✅ Seeded %d default currencies", seeded)
		} else {
			log.Println("✅ Default currencies already exist")
		}

		pairStore = store.NewPairStore(postgresStore.GetDB())

		defaultPairs := []*models.Pair{
			{Base: "BTC", Quote: "USDT"},
			{Base: "ETH", Quote: "USDT"},
			{Base: "BTC", Quote: "USD"},
			{Base: "ETH", Quote: "USD"},
		}
		seeded, err = pairStore.SeedDefaultPairs(context.Background(), defaultPairs)
		if err != nil {
			log.Printf("⚠️ Failed to seed default pairs: %v", err)
		} else if seeded > 0 {
			log.Printf("✅ Seeded %d default pairs", seeded)
		} else {
			log.Println("✅ Default pairs already exist")
		}
	} else {
		log.Println("⚠️ Currency store not available (PostgreSQL required)")
	}

	var publisher *messaging.Publisher
	publisher, err = messaging.NewPublisher(cfg.RabbitMQURL, cfg.RabbitMQExchange)
	if err != nil {
		log.Printf("⚠️ RabbitMQ publisher not available: %v", err)
		publisher = nil
	} else {
		log.Println("✅ RabbitMQ publisher connected")
		defer publisher.Close()
	}

	var consumer *messaging.Consumer
	if publisher != nil && postgresStore != nil {
		consumer, err = messaging.NewConsumer(cfg.RabbitMQURL, postgresStore, cfg.WorkerCount)
		if err != nil {
			log.Printf("⚠️ RabbitMQ consumer not available: %v", err)
		} else {
			if err := consumer.Start(cfg.RabbitMQExchange); err != nil {
				log.Printf("⚠️ Failed to start consumer: %v", err)
			} else {
				log.Println("✅ RabbitMQ consumer started")
				defer consumer.Stop()
			}
		}
	}

	obManager.SetTradeCallback(func(pair string, trade *engine.TradeResult) {
		wsTrade := &models.Trade{
			BuyOrderID:  trade.BuyOrderID,
			SellOrderID: trade.SellOrderID,
			Price:       trade.Price,
			Quantity:    trade.Quantity,
			CreatedAt:   time.Now(),
		}

		wsHub.BroadcastTrade(pair, wsTrade)

		if redisCache != nil {
			redisCache.AddRecentTrade(pair, *wsTrade)
		}

		if publisher != nil {
			publisher.Publish("trade.executed", wsTrade)
		}
	})

	obManager.SetOrderCallback(func(pair string, order *engine.OrderWrapper) {
		if publisher != nil {
			publisher.Publish("order.placed", order)
		}
	})

	var recoveryManager *cache.RecoveryManager
	if redisCache != nil {
		recoveryManager = cache.NewRecoveryManager(redisCache, nil)

		result, err := recoveryManager.RecoverOrderBook(obManager)
		if err != nil {
			log.Printf("⚠️ Recovery error: %v", err)
		} else if result.Success && result.SnapshotUsed {
			log.Printf("✅ Recovered from snapshot: %d pairs, %d orders, %d trades (took %v)",
				result.PairsRecovered, result.OrdersLoaded, result.TradesLoaded, result.RecoveryTime)
		}

		go recoveryManager.StartAutoSnapshot(obManager)
		defer func() {
			if recoveryManager != nil {
				recoveryManager.Stop()
			}
		}()
	}

	router := gin.Default()
	appMetrics := metrics.NewMetrics()
	api.RegisterRoutes(router, obManager, redisCache, wsHub, currencyStore, pairStore, appMetrics)

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("\n🛑 Shutting down...")
		os.Exit(0)
	}()

	log.Printf("🚀 Limit Order Book running on %s", cfg.ServerPort)
	log.Printf("📱 WebSocket endpoint: ws://%s/ws/{{pair}}", cfg.ServerPort)
	router.Run(cfg.ServerPort)
}
