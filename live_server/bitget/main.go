package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"
)

// Config holds the configuration from config.yaml
type Config struct {
	Bitget struct {
		APIKey     string `yaml:"api_key"`
		SecretKey  string `yaml:"secret_key"`
		Passphrase string `yaml:"passphrase"`
	} `yaml:"bitget"`
	Trading struct {
		Symbol string `yaml:"symbol"`
	} `yaml:"trading"`
	Server struct {
		Port int `yaml:"live_port"`
	} `yaml:"server"`
}

// WebSocket Message Types
const (
	MsgTypeKline      = "kline"
	MsgTypeAccount    = "account"
	MsgTypeOrder      = "orders"
	MsgTypeTradeEvent = "trade_event"
	MsgTypePosition   = "position"
)

// Bitget WebSocket constants
const (
	BitgetWSPublic  = "wss://ws.bitget.com/v2/ws/public"
	BitgetWSPrivate = "wss://ws.bitget.com/v2/ws/private"
	BitgetRestAPI   = "https://api.bitget.com"
)

// Bitget REST API Response Structures
type BitgetAccountResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		MarginCoin           string `json:"marginCoin"`
		Locked               string `json:"locked"`
		Available            string `json:"available"`
		CrossedMaxAvailable  string `json:"crossedMaxAvailable"`
		IsolatedMaxAvailable string `json:"isolatedMaxAvailable"`
		MaxTransferOut       string `json:"maxTransferOut"`
		AccountEquity        string `json:"accountEquity"`
		UsdtEquity           string `json:"usdtEquity"`
		BtcEquity            string `json:"btcEquity"`
		CrossedRiskRate      string `json:"crossedRiskRate"`
		UnrealizedPL         string `json:"unrealizedPL"`
		CrossedUnrealizedPL  string `json:"crossedUnrealizedPL"`
		IsolatedUnrealizedPL string `json:"isolatedUnrealizedPL"`
	} `json:"data"`
}

type BitgetCandlesResponse struct {
	Code string     `json:"code"`
	Msg  string     `json:"msg"`
	Data [][]string `json:"data"` // [timestamp, open, high, low, close, baseVol, usdtVol]
}

// WSMessage is the standard message format sent to frontend
type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Bitget WebSocket Request/Response structures
type BitgetWSRequest struct {
	Op   string                   `json:"op"`
	Args []map[string]interface{} `json:"args"`
}

type BitgetWSAuth struct {
	Op   string                   `json:"op"`
	Args []map[string]interface{} `json:"args"`
}

type BitgetKlineData struct {
	Ts   []interface{} `json:"ts"`
	Data [][]string    `json:"data"` // [timestamp, open, high, low, close, volume, ...]
	Arg  struct {
		InstType string `json:"instType"`
		Channel  string `json:"channel"`
		InstId   string `json:"instId"`
	} `json:"arg"`
}

type BitgetOrderData struct {
	Action string `json:"action"`
	Arg    struct {
		InstType string `json:"instType"`
		Channel  string `json:"channel"`
		InstId   string `json:"instId"`
	} `json:"arg"`
	Data []struct {
		OrderId       string `json:"orderId"`
		ClientOid     string `json:"clientOid"`
		Price         string `json:"price"`
		Size          string `json:"size"`
		NotionalUsd   string `json:"notionalUsd"`
		OrderType     string `json:"orderType"`
		Force         string `json:"force"`
		Side          string `json:"side"`
		PosSide       string `json:"posSide"`
		TradeSide     string `json:"tradeSide"`
		AccBaseVolume string `json:"accBaseVolume"`
		Status        string `json:"status"`
		CTime         string `json:"cTime"`
		UTime         string `json:"uTime"`
	} `json:"data"`
	Ts int64 `json:"ts"`
}

// Client wrapper with write mutex
type Client struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (c *Client) WriteJSON(v interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

// Global Hub to manage clients
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan WSMessage
	register   chan *Client
	unregister chan *Client
	mu         sync.Mutex
}

var hub = Hub{
	broadcast:  make(chan WSMessage),
	register:   make(chan *Client),
	unregister: make(chan *Client),
	clients:    make(map[*Client]bool),
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Println("New client connected")
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.conn.Close()
			}
			h.mu.Unlock()
			log.Println("Client disconnected")
		case message := <-h.broadcast:
			h.mu.Lock()
			clientsList := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				clientsList = append(clientsList, client)
			}
			h.mu.Unlock()

			// Write to clients outside the lock
			for _, client := range clientsList {
				err := client.WriteJSON(message)
				if err != nil {
					log.Printf("Error writing to client: %v", err)
					h.mu.Lock()
					delete(h.clients, client)
					h.mu.Unlock()
					client.conn.Close()
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all CORS for demo purposes
	},
}

// generateSignature generates Bitget API signature
func generateSignature(timestamp, method, requestPath, body, secretKey string) string {
	message := timestamp + method + requestPath + body
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// makeRestRequest makes an authenticated REST API request to Bitget
func makeRestRequest(config *Config, method, path string) ([]byte, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	sign := generateSignature(timestamp, method, path, "", config.Bitget.SecretKey)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(method, BitgetRestAPI+path, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("ACCESS-KEY", config.Bitget.APIKey)
	req.Header.Set("ACCESS-SIGN", sign)
	req.Header.Set("ACCESS-PASSPHRASE", config.Bitget.Passphrase)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("locale", "en-US")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

// fetchAccountInfo fetches account information from Bitget
func fetchAccountInfo(config *Config) (*BitgetAccountResponse, error) {
	body, err := makeRestRequest(config, "GET", "/api/v2/mix/account/accounts?productType=USDT-FUTURES")
	if err != nil {
		return nil, err
	}

	var accountResp BitgetAccountResponse
	if err := json.Unmarshal(body, &accountResp); err != nil {
		return nil, err
	}

	return &accountResp, nil
}

// fetchHistoryCandles fetches historical K-line data from Bitget
func fetchHistoryCandles(config *Config, symbol string, limit int) (*BitgetCandlesResponse, error) {
	path := fmt.Sprintf("/api/v2/mix/market/candles?symbol=%s&productType=USDT-FUTURES&granularity=1m&limit=%d", symbol, limit)
	body, err := makeRestRequest(config, "GET", path)
	if err != nil {
		return nil, err
	}

	var candlesResp BitgetCandlesResponse
	if err := json.Unmarshal(body, &candlesResp); err != nil {
		return nil, err
	}

	return &candlesResp, nil
}

// BitgetOpenOrdersResponse structure for open orders
type BitgetOpenOrdersResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		EntrustedList []struct {
			OrderId     string `json:"orderId"`
			ClientOid   string `json:"clientOid"`
			Symbol      string `json:"symbol"`
			OrderType   string `json:"orderType"`
			Side        string `json:"side"`
			Price       string `json:"price"`
			Size        string `json:"size"`
			Status      string `json:"status"`
			BaseVolume  string `json:"baseVolume"`
			QuoteVolume string `json:"quoteVolume"`
			CTime       string `json:"cTime"`
			UTime       string `json:"uTime"`
		} `json:"entrustedList"`
	} `json:"data"`
}

// fetchOpenOrders fetches current open orders from Bitget
func fetchOpenOrders(config *Config, symbol string) (*BitgetOpenOrdersResponse, error) {
	path := fmt.Sprintf("/api/v2/mix/order/orders-pending?symbol=%s&productType=USDT-FUTURES", symbol)
	body, err := makeRestRequest(config, "GET", path)
	if err != nil {
		return nil, err
	}

	var ordersResp BitgetOpenOrdersResponse
	if err := json.Unmarshal(body, &ordersResp); err != nil {
		return nil, err
	}

	// Debug: log raw response if no orders or non-success code to help diagnose frontend missing orders
	if ordersResp.Code != "00000" || len(ordersResp.Data.EntrustedList) == 0 {
		log.Printf("Debug fetchOpenOrders - raw response: %s", string(body))
	}

	return &ordersResp, nil
}

func main() {
	// Load Config
	configFile := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	data, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}

	if config.Trading.Symbol == "" {
		config.Trading.Symbol = "ETHUSDT"
	}
	// Default port if not in config
	if config.Server.Port == 0 {
		config.Server.Port = 9798
	}

	log.Printf("Starting Bitget Live Server for %s on port %d...", config.Trading.Symbol, config.Server.Port)

	// Start Hub
	go hub.run()

	// Function to fetch and send initial data - Bitget REST API calls would go here
	sendInitialData := func(client *Client) {
		// Fetch account information
		accountResp, err := fetchAccountInfo(&config)
		if err != nil {
			log.Printf("Error fetching account info: %v", err)
		} else if accountResp.Code == "00000" && len(accountResp.Data) > 0 {
			for _, account := range accountResp.Data {
				// Format compatible with Binance: asset, free, balance
				asset := strings.ToUpper(account.MarginCoin)
				client.WriteJSON(WSMessage{
					Type: MsgTypeAccount,
					Data: map[string]interface{}{
						"asset":         asset,
						"free":          account.Available,
						"balance":       account.AccountEquity,
						"marginBalance": account.AccountEquity,
						"symbol":        strings.ToUpper(config.Trading.Symbol),
					},
				})
				log.Printf("📊 Account: %s, Balance: %s, Available: %s",
					account.MarginCoin, account.AccountEquity, account.Available)
			}
		}

		// Fetch current open orders
		ordersResp, err := fetchOpenOrders(&config, config.Trading.Symbol)
		if err != nil {
			log.Printf("Error fetching open orders: %v", err)
		} else if ordersResp.Code == "00000" && len(ordersResp.Data.EntrustedList) > 0 {
			// Send all orders in a single message (Binance compatible format)
			var orderList []map[string]interface{}
			for _, order := range ordersResp.Data.EntrustedList {
				// Normalize Bitget values to Binance-like uppercase conventions
				sideNorm := strings.ToUpper(order.Side)
				statusLower := strings.ToLower(order.Status)
				statusNorm := strings.ToUpper(order.Status)
				switch statusLower {
				case "filled":
					statusNorm = "FILLED"
				case "partial-fill", "partial_filled":
					statusNorm = "PARTIALLY_FILLED"
				case "live", "open":
					statusNorm = "NEW"
				case "canceled", "cancel", "cancelled":
					statusNorm = "CANCELED"
				default:
					statusNorm = strings.ToUpper(statusLower)
				}

				orderList = append(orderList, map[string]interface{}{
					"id":     order.OrderId,
					"price":  order.Price,
					"side":   sideNorm,
					"status": statusNorm,
					"type":   order.OrderType,
					"symbol": strings.ToUpper(config.Trading.Symbol),
				})
			}
			client.WriteJSON(WSMessage{
				Type: MsgTypeOrder,
				Data: orderList,
			})
			log.Printf("📋 Sent %d open orders", len(ordersResp.Data.EntrustedList))
		}
	}

	// Function to fetch and broadcast history - Bitget REST API
	sendHistory := func(client *Client) {
		// Fetch K-line history from Bitget REST API
		candlesResp, err := fetchHistoryCandles(&config, config.Trading.Symbol, 100)
		if err != nil {
			log.Printf("Error fetching history candles: %v", err)
			return
		}

		if candlesResp.Code == "00000" && len(candlesResp.Data) > 0 {
			var historyData []map[string]interface{}
			for _, candle := range candlesResp.Data {
				if len(candle) >= 6 {
					// Bitget format: [timestamp, open, high, low, close, baseVol, usdtVol]
					historyData = append(historyData, map[string]interface{}{
						"time":   parseTimestamp(candle[0]),
						"open":   candle[1],
						"high":   candle[2],
						"low":    candle[3],
						"close":  candle[4],
						"volume": candle[5],
					})
				}
			}

			// Send all history in one message
			client.WriteJSON(WSMessage{
				Type: "history",
				Data: historyData,
			})
			log.Printf("📈 Sent %d historical candles", len(historyData))
		}
	}

	// 1. Monitor Kline (1m) - Bitget Public WebSocket
	go func() {
		for {
			conn, _, err := websocket.DefaultDialer.Dial(BitgetWSPublic, nil)
			if err != nil {
				log.Printf("Error connecting to Bitget public WebSocket: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			// Subscribe to candle channel
			subscribeMsg := BitgetWSRequest{
				Op: "subscribe",
				Args: []map[string]interface{}{
					{
						"instType": "USDT-FUTURES",
						"channel":  "candle1m",
						"instId":   config.Trading.Symbol,
					},
				},
			}

			if err := conn.WriteJSON(subscribeMsg); err != nil {
				log.Printf("Error subscribing to Bitget kline: %v", err)
				conn.Close()
				time.Sleep(5 * time.Second)
				continue
			}

			log.Printf("✅ Subscribed to Bitget Kline: %s", config.Trading.Symbol)

			// Ping/Pong handler
			go func() {
				ticker := time.NewTicker(20 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
						log.Printf("Ping error: %v", err)
						return
					}
				}
			}()

			// Read messages
			for {
				_, message, err := conn.ReadMessage()
				if err != nil {
					log.Printf("Bitget kline read error: %v", err)
					break
				}

				// Handle pong response
				if string(message) == "pong" {
					continue
				}

				var rawMsg map[string]interface{}
				if err := json.Unmarshal(message, &rawMsg); err != nil {
					continue
				}

				// Check if it's kline data
				if rawMsg["arg"] != nil && rawMsg["data"] != nil {
					arg, ok := rawMsg["arg"].(map[string]interface{})
					if !ok {
						continue
					}
					if arg["channel"] == "candle1m" {
						dataArray, ok := rawMsg["data"].([]interface{})
						if !ok || len(dataArray) == 0 {
							continue
						}
						for _, item := range dataArray {
							kline, ok := item.([]interface{})
							if !ok || len(kline) < 6 {
								continue
							}
							// Bitget format: [timestamp, open, high, low, close, volume, ...]
							hub.broadcast <- WSMessage{
								Type: MsgTypeKline,
								Data: map[string]interface{}{
									"time":   parseTimestamp(kline[0]),
									"open":   kline[1],
									"high":   kline[2],
									"low":    kline[3],
									"close":  kline[4],
									"volume": kline[5],
								},
							}
						}
					}
				}
			}

			conn.Close()
			log.Println("Bitget kline WebSocket disconnected, reconnecting...")
			time.Sleep(3 * time.Second)
		}
	}()

	// 2. Monitor User Data (Orders/Trades) - Bitget Private WebSocket
	go func() {
		for {
			conn, _, err := websocket.DefaultDialer.Dial(BitgetWSPrivate, nil)
			if err != nil {
				log.Printf("Error connecting to Bitget private WebSocket: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			// Authenticate
			timestamp := fmt.Sprintf("%d", time.Now().Unix())
			sign := generateSignature(timestamp, "GET", "/user/verify", "", config.Bitget.SecretKey)

			authMsg := BitgetWSAuth{
				Op: "login",
				Args: []map[string]interface{}{
					{
						"apiKey":     config.Bitget.APIKey,
						"passphrase": config.Bitget.Passphrase,
						"timestamp":  timestamp,
						"sign":       sign,
					},
				},
			}

			if err := conn.WriteJSON(authMsg); err != nil {
				log.Printf("Error sending auth to Bitget: %v", err)
				conn.Close()
				time.Sleep(5 * time.Second)
				continue
			}

			// Wait for auth response
			_, authResp, err := conn.ReadMessage()
			if err != nil {
				log.Printf("Error reading auth response: %v", err)
				conn.Close()
				time.Sleep(5 * time.Second)
				continue
			}

			var authResult map[string]interface{}
			json.Unmarshal(authResp, &authResult)

			// Check auth response (code can be string "0" or int 0)
			codeOk := false
			if code, ok := authResult["code"]; ok {
				switch v := code.(type) {
				case string:
					codeOk = (v == "0")
				case float64:
					codeOk = (v == 0)
				case int:
					codeOk = (v == 0)
				}
			}

			if authResult["event"] != "login" || !codeOk {
				log.Printf("❌ Bitget auth failed: %s", string(authResp))
				conn.Close()
				time.Sleep(5 * time.Second)
				continue
			}

			log.Println("✅ Bitget authenticated successfully")

			// Subscribe to multiple channels: orders, account, positions
			subscribeMsg := BitgetWSRequest{
				Op: "subscribe",
				Args: []map[string]interface{}{
					{
						"instType": "USDT-FUTURES",
						"channel":  "orders",
						"instId":   "default", // default means all symbols
					},
					{
						"instType": "USDT-FUTURES",
						"channel":  "account",
						"coin":     "USDT", // Monitor USDT account
					},
					{
						"instType": "USDT-FUTURES",
						"channel":  "positions",
						"instId":   "default", // default means all symbols
					},
				},
			}

			if err := conn.WriteJSON(subscribeMsg); err != nil {
				log.Printf("Error subscribing to channels: %v", err)
				conn.Close()
				time.Sleep(5 * time.Second)
				continue
			}

			log.Println("✅ Subscribed to Bitget Orders, Account, and Positions Channels")

			// Function to fetch and broadcast account info
			broadcastAccountInfo := func() {
				accountResp, err := fetchAccountInfo(&config)
				if err != nil {
					log.Printf("Error fetching account info: %v", err)
					return
				}
				if accountResp.Code == "00000" && len(accountResp.Data) > 0 {
					for _, account := range accountResp.Data {
						asset := strings.ToUpper(account.MarginCoin)
						hub.broadcast <- WSMessage{
							Type: MsgTypeAccount,
							Data: map[string]interface{}{
								"asset":         asset,
								"free":          account.Available,
								"balance":       account.AccountEquity,
								"marginBalance": account.AccountEquity,
								"symbol":        strings.ToUpper(config.Trading.Symbol),
							},
						}
						log.Printf("💰 Account Info: %s, Balance: %s, Free: %s",
							asset, account.AccountEquity, account.Available)
					}
				}
			}

			// Fetch initial account info via REST API and broadcast
			go func() {
				time.Sleep(500 * time.Millisecond) // Wait a bit for subscription to be processed
				broadcastAccountInfo()
			}()

			// Periodic account polling (every 5 seconds) as fallback for WebSocket updates
			go func() {
				ticker := time.NewTicker(5 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					broadcastAccountInfo()
				}
			}()

			// Ping/Pong handler
			go func() {
				ticker := time.NewTicker(20 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
						log.Printf("Ping error: %v", err)
						return
					}
				}
			}()

			// Read messages
			for {
				_, message, err := conn.ReadMessage()
				if err != nil {
					log.Printf("Bitget orders read error: %v", err)
					break
				}

				// Handle pong response
				if string(message) == "pong" {
					continue
				}

				var rawMsg map[string]interface{}
				if err := json.Unmarshal(message, &rawMsg); err != nil {
					continue
				}

				// Debug: log raw message for account channel
				if rawMsg["arg"] != nil {
					arg, ok := rawMsg["arg"].(map[string]interface{})
					if ok {
						channel, _ := arg["channel"].(string)
						if channel == "account" {
							log.Printf("🔍 Raw account message: %s", string(message))
						}
					}
				}

				// Handle different channel updates
				if rawMsg["arg"] != nil && rawMsg["data"] != nil {
					arg, ok := rawMsg["arg"].(map[string]interface{})
					if !ok {
						continue
					}

					channel, _ := arg["channel"].(string)

					switch channel {
					case "orders":
						// Handle order updates
						dataArray, ok := rawMsg["data"].([]interface{})
						if !ok || len(dataArray) == 0 {
							continue
						}
						for _, item := range dataArray {
							order, ok := item.(map[string]interface{})
							if !ok {
								continue
							}

							orderId, _ := order["orderId"].(string)
							price, _ := order["price"].(string)
							side, _ := order["side"].(string)
							status, _ := order["status"].(string)
							orderType, _ := order["orderType"].(string)
							accBaseVolume, _ := order["accBaseVolume"].(string)

							// Keep lowercase/originals for internal checks
							sideLower := strings.ToLower(side)
							statusLower := strings.ToLower(status)

							// Normalize to Binance-like values for frontend
							sideNorm := strings.ToUpper(side)
							statusNorm := strings.ToUpper(status)
							switch statusLower {
							case "filled":
								statusNorm = "FILLED"
							case "partial-fill", "partial_filled":
								statusNorm = "PARTIALLY_FILLED"
							case "live", "open":
								statusNorm = "NEW"
							case "canceled", "cancel", "cancelled":
								statusNorm = "CANCELED"
							default:
								statusNorm = strings.ToUpper(statusLower)
							}

							log.Printf("Bitget Order Update: %s %s @ %s, Status: %s",
								sideLower, orderType, price, statusLower)

							// Broadcast order update (normalized)
							hub.broadcast <- WSMessage{
								Type: MsgTypeOrder,
								Data: map[string]interface{}{
									"id":     orderId,
									"price":  price,
									"side":   sideNorm,
									"status": statusNorm,
									"type":   orderType,
									"symbol": strings.ToUpper(config.Trading.Symbol),
								},
							}

							// Broadcast trade event if order is filled (check original values)
							if (statusLower == "filled" || statusLower == "partial-fill") && accBaseVolume != "0" {
								tradeSide := "buy"
								if sideLower == "sell" {
									tradeSide = "sell"
								}

								hub.broadcast <- WSMessage{
									Type: MsgTypeTradeEvent,
									Data: map[string]interface{}{
										"side":   tradeSide,
										"price":  price,
										"amount": accBaseVolume,
										"symbol": order["instId"],
										"time":   time.Now().Unix(),
									},
								}

								log.Printf("🔔 Bitget Trade Executed: %s %s @ %s", tradeSide, accBaseVolume, price)
							}
						}

					case "account":
						// Handle account balance updates
						log.Printf("🔔 Received account channel update")
						dataArray, ok := rawMsg["data"].([]interface{})
						if !ok || len(dataArray) == 0 {
							log.Printf("⚠️ Account data is not an array or is empty")
							continue
						}
						for _, item := range dataArray {
							account, ok := item.(map[string]interface{})
							if !ok {
								log.Printf("⚠️ Account item is not a map")
								continue
							}

							// Debug: log all account fields
							log.Printf("🔍 Account data keys: %v", getMapKeys(account))

							marginCoin, _ := account["marginCoin"].(string)
							available, _ := account["available"].(string)
							locked, _ := account["locked"].(string)

							// Try multiple possible field names for equity/balance
							var equity string
							if eq, ok := account["accountEquity"].(string); ok && eq != "" {
								equity = eq
							} else if eq, ok := account["equity"].(string); ok && eq != "" {
								equity = eq
							} else if eq, ok := account["balance"].(string); ok && eq != "" {
								equity = eq
							} else if eq, ok := account["usdtEquity"].(string); ok && eq != "" {
								equity = eq
							} else if eq, ok := account["accountEquity"].(float64); ok {
								equity = fmt.Sprintf("%.8f", eq)
							} else if eq, ok := account["equity"].(float64); ok {
								equity = fmt.Sprintf("%.8f", eq)
							}

							// If equity is still empty, use available as fallback
							if equity == "" && available != "" {
								equity = available
								log.Printf("⚠️ Using available as balance fallback")
							}

							log.Printf("💰 Account Update: %s Balance: %s, Free: %s, Locked: %s",
								marginCoin, equity, available, locked)

							// Only broadcast if we have valid data
							if marginCoin != "" && equity != "" {
								hub.broadcast <- WSMessage{
									Type: MsgTypeAccount,
									Data: map[string]interface{}{
										"asset":         strings.ToUpper(marginCoin),
										"free":          available,
										"balance":       equity,
										"marginBalance": equity,
										"symbol":        strings.ToUpper(config.Trading.Symbol),
									},
								}
								log.Printf("✅ Broadcasted account update for %s", strings.ToUpper(marginCoin))
							} else {
								log.Printf("⚠️ Skipping account update - missing marginCoin or equity")
							}
						}

					case "positions":
						// Handle position updates
						dataArray, ok := rawMsg["data"].([]interface{})
						if !ok || len(dataArray) == 0 {
							continue
						}
						for _, item := range dataArray {
							position, ok := item.(map[string]interface{})
							if !ok {
								continue
							}

							instId, _ := position["instId"].(string)
							positionSide, _ := position["holdSide"].(string)
							total, _ := position["total"].(string)
							available, _ := position["available"].(string)
							openPriceAvg, _ := position["openPriceAvg"].(string)
							unrealizedPL, _ := position["unrealizedPL"].(string)
							leverage, _ := position["leverage"].(string)

							// Only broadcast if there's an actual position
							if total != "0" && total != "" {
								log.Printf("📊 Position Update: %s %s, Size: %s, Entry: %s, PnL: %s",
									instId, positionSide, total, openPriceAvg, unrealizedPL)

								hub.broadcast <- WSMessage{
									Type: MsgTypePosition,
									Data: map[string]interface{}{
										"symbol":       strings.ToUpper(instId),
										"holdSide":     positionSide,
										"total":        total,
										"available":    available,
										"openPriceAvg": openPriceAvg,
										"unrealizedPL": unrealizedPL,
										"leverage":     leverage,
									},
								}
							}
						}
					}
				}
			}

			conn.Close()
			log.Println("Bitget orders WebSocket disconnected, reconnecting...")
			time.Sleep(3 * time.Second)
		}
	}()

	// HTTP WebSocket Handler
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)
			return
		}
		client := &Client{conn: ws}

		// Register client
		hub.register <- client

		// Send initial data (account + open orders)
		go sendInitialData(client)

		// Send history asynchronously
		go sendHistory(client)

		// Read loop
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				hub.unregister <- client
				break
			}
		}
	})

	log.Printf("🚀 Bitget Live Server started on http://0.0.0.0:%d/ws", config.Server.Port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", config.Server.Port), nil))
}

// Helper function to parse timestamp
func parseTimestamp(ts interface{}) int64 {
	switch v := ts.(type) {
	case string:
		var timestamp int64
		fmt.Sscanf(v, "%d", &timestamp)
		return timestamp / 1000 // Convert to seconds
	case float64:
		return int64(v) / 1000
	case int64:
		return v / 1000
	default:
		return 0
	}
}

// Helper function to get map keys for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
